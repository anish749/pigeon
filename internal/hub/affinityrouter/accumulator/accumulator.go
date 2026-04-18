// Package accumulator implements the signal accumulation and routing logic.
// It receives signals, applies conversation affinity (fast path), buffers
// signals, and triggers batch classification when thresholds are reached.
package accumulator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/classifier"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Accumulator receives signals, applies fast-path affinity routing, and
// triggers batch classification when thresholds are reached.
type Accumulator struct {
	store        *affinityrouter.Store
	classifier   *classifier.BatchClassifier
	threshold    models.BatchThreshold
	approvalMode models.ApprovalMode
	logger       *slog.Logger

	// Stats
	totalSignals    int
	fastPathRouted  int
	batchClassified int
	classifierCalls int
}

// New creates an accumulator.
func New(store *affinityrouter.Store, cls *classifier.BatchClassifier, threshold models.BatchThreshold, approvalMode models.ApprovalMode, logger *slog.Logger) *Accumulator {
	return &Accumulator{
		store:        store,
		classifier:   cls,
		threshold:    threshold,
		approvalMode: approvalMode,
		logger:       logger,
	}
}

// Receive processes a single incoming signal. It applies the fast path
// (conversation affinity) to tag the signal, buffers it, and triggers
// batch classification if the threshold is reached.
func (a *Accumulator) Receive(ctx context.Context, sig models.Signal) error {
	a.totalSignals++

	// Ensure the workspace has a default workstream.
	workspace := sig.Account.Name
	a.store.EnsureDefaultWorkstream(workspace)

	// Fast path: look up conversation affinities (multi-routing).
	key := models.ConversationKey{
		Workspace:    workspace,
		Conversation: sig.Conversation,
	}
	aff := a.store.GetAffinities(key)
	if aff != nil {
		ids := aff.WorkstreamIDs()
		if len(ids) > 0 {
			sig.WorkstreamIDs = ids
			a.fastPathRouted++
		}
	}
	if len(sig.WorkstreamIDs) == 0 {
		sig.WorkstreamIDs = []string{models.DefaultWorkstreamID(workspace)}
	}

	// Record the signal against all affiliated workstreams.
	a.store.RecordSignal(sig)

	// Buffer for batch classification.
	a.store.BufferSignal(sig)

	// Check if any conversation's buffer is ready for classification.
	ready := a.store.ReadyBuffers(a.threshold, sig.Ts)
	var errs []error
	for _, rk := range ready {
		if err := a.classifyBuffer(ctx, rk, sig.Ts); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ProcessPending classifies all remaining buffers that have pending signals.
// Call this at the end of a replay to flush everything.
func (a *Accumulator) ProcessPending(ctx context.Context, now models.Signal) error {
	forceThreshold := models.BatchThreshold{MinSignals: 1}
	ready := a.store.ReadyBuffers(forceThreshold, now.Ts)
	var errs []error
	for _, key := range ready {
		if err := a.classifyBuffer(ctx, key, now.Ts); err != nil {
			errs = append(errs, fmt.Errorf("classify %s/%s: %w", key.Workspace, key.Conversation, err))
		}
	}
	return errors.Join(errs...)
}

// classifyBuffer runs the batch classifier on a conversation's buffered signals.
func (a *Accumulator) classifyBuffer(ctx context.Context, key models.ConversationKey, now time.Time) error {
	signals := a.store.DrainBuffer(key, now)
	if len(signals) == 0 {
		return nil
	}

	a.classifierCalls++

	// Get active workstreams and current affinities for context.
	active := a.store.ActiveWorkstreams(key.Workspace)
	var currentAffinityIDs []string
	if aff := a.store.GetAffinities(key); aff != nil {
		currentAffinityIDs = aff.WorkstreamIDs()
	}

	result, err := a.classifier.Classify(ctx, key, signals, active, currentAffinityIDs)
	if err != nil {
		return fmt.Errorf("classifier: %w", err)
	}

	a.batchClassified += len(signals)
	return a.applyResult(ctx, key, signals, result)
}

func (a *Accumulator) applyResult(_ context.Context, key models.ConversationKey, signals []models.Signal, result *classifier.Result) error {
	// Handle new workstream proposal — queue for user confirmation.
	if result.NewWorkstreamName != "" {
		proposal := &models.Proposal{
			Type:              models.ProposalCreate,
			SuggestedName:     result.NewWorkstreamName,
			SuggestedFocus:    result.NewWorkstreamFocus,
			Workspace:         key.Workspace,
			TriggeringSignals: signals,
			Confidence:        result.Confidence,
			Reasoning:         result.Reasoning,
		}

		if a.approvalMode == models.AutoApprove {
			// Auto-approve: create immediately.
			ws := &models.Workstream{
				ID:        generateWorkstreamID(result.NewWorkstreamName),
				Name:      result.NewWorkstreamName,
				Workspace: key.Workspace,
				State:     models.StateActive,
				Focus:     result.NewWorkstreamFocus,
				Created:   signals[0].Ts,
			}
			if err := a.store.CreateWorkstream(ws); err != nil {
				// ID collision — use existing.
				ws = a.store.GetWorkstream(ws.ID)
				if ws == nil {
					return fmt.Errorf("create workstream failed: %s", result.NewWorkstreamName)
				}
			} else {
				a.logger.Info("new workstream created (auto-approved)",
					"workspace", key.Workspace,
					"name", ws.Name,
					"id", ws.ID,
				)
			}
			proposal.State = models.ProposalApproved
			// Add the new workstream to the routing set.
			result.WorkstreamIDs = append(result.WorkstreamIDs, ws.ID)
		} else {
			// Queue for user confirmation.
			a.store.AddProposal(proposal)
			a.logger.Info("workstream proposed (pending confirmation)",
				"workspace", key.Workspace,
				"name", result.NewWorkstreamName,
				"confidence", result.Confidence,
			)
		}
	}

	// Route signals to the classified workstream(s).
	if len(result.WorkstreamIDs) > 0 {
		for i := range signals {
			signals[i].WorkstreamIDs = result.WorkstreamIDs
			a.store.RecordSignal(signals[i])
		}

		a.logger.Info("batch classified",
			"workspace", key.Workspace,
			"conversation", key.Conversation,
			"workstreams", strings.Join(result.WorkstreamIDs, ", "),
			"signals", len(signals),
			"confidence", result.Confidence,
		)
	}

	return nil
}

func generateWorkstreamID(name string) string {
	var b strings.Builder
	b.WriteString("ws-")
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else if c >= 'A' && c <= 'Z' {
			b.WriteRune(c + 32)
		} else if c == ' ' || c == '-' || c == '_' {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// AccumulatorStats returns routing statistics.
type AccumulatorStats struct {
	TotalSignals    int
	FastPathRouted  int
	BatchClassified int
	ClassifierCalls int
}

// Stats returns routing statistics.
func (a *Accumulator) Stats() AccumulatorStats {
	return AccumulatorStats{
		TotalSignals:    a.totalSignals,
		FastPathRouted:  a.fastPathRouted,
		BatchClassified: a.batchClassified,
		ClassifierCalls: a.classifierCalls,
	}
}
