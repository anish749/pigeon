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
	store      *affinityrouter.Store
	classifier *classifier.BatchClassifier
	threshold  models.BatchThreshold
	logger     *slog.Logger

	// Stats
	totalSignals     int
	fastPathRouted   int
	batchClassified  int
	classifierCalls  int
}

// New creates an accumulator.
func New(store *affinityrouter.Store, cls *classifier.BatchClassifier, threshold models.BatchThreshold, logger *slog.Logger) *Accumulator {
	return &Accumulator{
		store:      store,
		classifier: cls,
		threshold:  threshold,
		logger:     logger,
	}
}

// Receive processes a single incoming signal. It applies the fast path
// (conversation affinity) to tag the signal, buffers it, and triggers
// batch classification if the threshold is reached.
//
// In replay mode, call ProcessPending after all signals have been received
// to classify any remaining buffers.
func (a *Accumulator) Receive(ctx context.Context, sig models.Signal) error {
	a.totalSignals++

	// Ensure the workspace has a default workstream.
	workspace := sig.Account.Name
	a.store.EnsureDefaultWorkstream(workspace)

	// Fast path: look up conversation affinity.
	key := models.ConversationKey{
		Workspace:    workspace,
		Conversation: sig.Conversation,
	}
	aff := a.store.GetAffinity(key)
	if aff != nil && aff.WorkstreamID != "" {
		// Conversation has an existing affinity — tag the signal.
		sig.WorkstreamID = aff.WorkstreamID
		a.fastPathRouted++
	} else {
		// No affinity — route to default.
		sig.WorkstreamID = models.DefaultWorkstreamID(workspace)
	}

	// Record the signal (updates workstream counters + affinity).
	a.store.RecordSignal(sig)

	// Buffer for batch classification.
	a.store.BufferSignal(sig)

	// Check if this conversation's buffer is ready for classification.
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
	// Use a zero threshold to force all non-empty buffers.
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

	// Get active workstreams for this workspace.
	active := a.store.ActiveWorkstreams(key.Workspace)

	// Run classification.
	result, err := a.classifier.Classify(ctx, key, signals, active)
	if err != nil {
		// On failure, put signals back? No — they're already recorded.
		// Just log and move on. The signals stay tagged with their fast-path affinity.
		return fmt.Errorf("classifier: %w", err)
	}

	a.batchClassified += len(signals)

	// Apply the classification result.
	return a.applyResult(ctx, key, signals, result)
}

func (a *Accumulator) applyResult(_ context.Context, key models.ConversationKey, signals []models.Signal, result *classifier.Result) error {
	switch {
	case result.WorkstreamID != "":
		// Signals belong to an existing workstream.
		// If this is different from the fast-path assignment, update the affinity.
		currentAff := a.store.GetAffinity(key)
		if currentAff == nil || currentAff.WorkstreamID != result.WorkstreamID {
			a.logger.Info("affinity updated",
				"workspace", key.Workspace,
				"conversation", key.Conversation,
				"from", currentAffID(currentAff),
				"to", result.WorkstreamID,
				"confidence", result.Confidence,
			)
		}
		// Re-record all signals with the correct workstream.
		for i := range signals {
			signals[i].WorkstreamID = result.WorkstreamID
			a.store.RecordSignal(signals[i])
		}

	case result.NewWorkstreamName != "":
		// Classifier proposes a new workstream.
		ws := &models.Workstream{
			ID:        generateWorkstreamID(key.Workspace, result.NewWorkstreamName),
			Name:      result.NewWorkstreamName,
			Workspace: key.Workspace,
			State:     models.StateActive,
			Focus:     result.NewWorkstreamFocus,
			Created:   signals[0].Ts,
		}
		if err := a.store.CreateWorkstream(ws); err != nil {
			// ID collision — workstream already exists, route there.
			ws = a.store.GetWorkstream(ws.ID)
			if ws == nil {
				return fmt.Errorf("create workstream failed and get returned nil: %s", result.NewWorkstreamName)
			}
		}
		a.logger.Info("new workstream created",
			"workspace", key.Workspace,
			"name", ws.Name,
			"id", ws.ID,
			"conversation", key.Conversation,
			"signals", len(signals),
		)
		// Re-record signals under the new workstream.
		for i := range signals {
			signals[i].WorkstreamID = ws.ID
			a.store.RecordSignal(signals[i])
		}
	}

	return nil
}

func currentAffID(aff *models.ConversationAffinity) string {
	if aff == nil {
		return "(none)"
	}
	return aff.WorkstreamID
}

func generateWorkstreamID(_, name string) string {
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
