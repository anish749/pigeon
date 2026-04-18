// Package router routes signals to workstreams using conversation affinity
// as the fast path and batch classification as the slow path.
//
// The Router is read-only during routing — it never writes to the ledger or
// creates workstreams. The caller (replay/orchestrator) is responsible for
// recording decisions and acting on new-workstream proposals.
package router

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/classifier"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Router routes signals to workstreams.
// It uses conversation affinity for the fast path and delegates to the
// classifier for batch classification when the buffer threshold is met.
type Router struct {
	classifier *classifier.BatchClassifier
	logger     *slog.Logger

	// Internal state.
	affinities map[models.ConversationKey][]models.AffinityEntry // conversation → workstream weights
	buffers    map[models.ConversationKey]*buffer                // pending signals per conversation

	// Config.
	workspace config.WorkspaceName
	burstGap  time.Duration

	// Stats.
	stats Stats

	mu sync.RWMutex
}

type buffer struct {
	signals        []models.Signal
	lastClassified time.Time
}

// New creates a Router.
func New(cls *classifier.BatchClassifier, cfg models.Config, logger *slog.Logger) *Router {
	return &Router{
		classifier:      cls,
		logger:          logger,
		affinities:      make(map[models.ConversationKey][]models.AffinityEntry),
		buffers:         make(map[models.ConversationKey]*buffer),
		workspace: cfg.Workspace.Name,
		burstGap:  cfg.BurstGap,
	}
}

// RouteResult bundles the routing decision with an optional new workstream proposal.
type RouteResult struct {
	Decision models.RoutingDecision

	// Set if the classifier proposes a new workstream.
	// The Router does NOT create it — the caller passes this to the Manager.
	NewWorkstreamName  string
	NewWorkstreamFocus string
}

// Route takes a signal and returns a routing decision.
//
// Fast path: if the conversation has affinity, return immediately with
// Source=SourceAffinity and Confidence=1.0.
//
// The signal is always buffered. When the buffer threshold is met, the
// classifier is called and a classifier-based decision is returned.
//
// The workstreams parameter provides the current active workstreams for
// the signal's workspace (needed by the classifier).
//
// Returns one of:
//   - Decision with affinity-based workstream IDs (fast path)
//   - Decision with classifier-based workstream IDs (slow path, when buffer triggers)
//   - Decision with default workstream ID (no affinity, buffer not ready)
//
// If the classifier proposes a new workstream, the NewWorkstream fields
// on the returned RouteResult will be set, but the Router does NOT create it.
func (r *Router) Route(ctx context.Context, sig models.Signal, workstreams []models.Workstream) (*RouteResult, error) {
	key := models.ConversationKey{
		Account:      sig.Account,
		Conversation: sig.Conversation,
	}
	now := sig.Ts

	r.mu.Lock()
	defer r.mu.Unlock()

	// Fast path: conversation has affinity.
	var affinityResult *RouteResult
	if entries, ok := r.affinities[key]; ok && len(entries) > 0 {
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.WorkstreamID
		}
		affinityResult = &RouteResult{
			Decision: models.RoutingDecision{
				SignalID:      sig.ID,
				WorkstreamIDs: ids,
				Ts:            now,
			},
		}
		r.stats.FastPathRouted++
	}

	// Always buffer the signal.
	buf := r.buffers[key]
	if buf == nil {
		buf = &buffer{}
		r.buffers[key] = buf
	}
	buf.signals = append(buf.signals, sig)
	r.stats.BufferedSignals++

	// Check if buffer is ready for classification.
	if !r.bufferReady(buf, now) {
		// Buffer not ready — return affinity result or default.
		if affinityResult != nil {
			return affinityResult, nil
		}
		return &RouteResult{
			Decision: models.RoutingDecision{
				SignalID:      sig.ID,
				WorkstreamIDs: []string{models.DefaultWorkstreamID(r.workspace)},
				Ts:            now,
			},
		}, nil
	}

	// Drain the buffer and classify.
	signals := buf.signals
	buf.signals = nil
	buf.lastClassified = now

	var currentAffinityIDs []string
	if entries, ok := r.affinities[key]; ok {
		for _, e := range entries {
			currentAffinityIDs = append(currentAffinityIDs, e.WorkstreamID)
		}
	}

	r.stats.ClassifierCalls++

	// Release the lock during the classifier call (network/LLM).
	r.mu.Unlock()
	result, err := r.classifier.Classify(ctx, key, signals, workstreams, currentAffinityIDs)
	r.mu.Lock()

	if err != nil {
		// On classifier error, re-buffer the signals and fall back.
		buf.signals = append(signals, buf.signals...)
		buf.lastClassified = time.Time{} // reset so we retry
		r.stats.ClassifierCalls--        // don't count failed calls

		if affinityResult != nil {
			return affinityResult, nil
		}
		return nil, err
	}

	// If classifier returned nothing valid, route to default.
	wsIDs := result.WorkstreamIDs
	if len(wsIDs) == 0 && result.NewWorkstreamName == "" {
		wsIDs = []string{models.DefaultWorkstreamID(r.workspace)}
	}

	routeResult := &RouteResult{
		Decision: models.RoutingDecision{
			SignalID:      sig.ID,
			WorkstreamIDs: wsIDs,
			Ts:            now,
		},
		NewWorkstreamName:  result.NewWorkstreamName,
		NewWorkstreamFocus: result.NewWorkstreamFocus,
	}

	r.logger.Info("classified batch",
		"account", key.Account.Display(),
		"conversation", key.Conversation,
		"signals", len(signals),
		"workstreams", result.WorkstreamIDs,
		"new_workstream", result.NewWorkstreamName,
	)

	return routeResult, nil
}

// bufferReady reports whether the buffer should be drained for classification.
// A burst boundary is detected when the gap between the last buffered signal
// and the current signal (now) exceeds the configured burst gap threshold.
// This captures natural conversation pauses — classify the completed burst.
func (r *Router) bufferReady(buf *buffer, now time.Time) bool {
	if len(buf.signals) == 0 {
		return false
	}
	// The burst ended: gap from the last buffered signal to now exceeds the threshold.
	lastSignal := buf.signals[len(buf.signals)-1].Ts
	return now.Sub(lastSignal) >= r.burstGap
}

// UpdateAffinity updates the conversation->workstream affinity weights.
// Called by the orchestrator after recording a routing decision in the ledger.
func (r *Router) UpdateAffinity(key models.ConversationKey, workstreamID string, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.affinities[key]
	for i, e := range entries {
		if e.WorkstreamID == workstreamID {
			entries[i].Strength++
			entries[i].LastSignal = ts
			r.affinities[key] = entries
			return
		}
	}
	r.affinities[key] = append(entries, models.AffinityEntry{
		WorkstreamID: workstreamID,
		Strength:     1,
		LastSignal:   ts,
	})
}

// FlushBuffers classifies all remaining buffered signals regardless of gap.
// Call at the end of a replay to ensure the last burst in every conversation
// gets classified.
func (r *Router) FlushBuffers(ctx context.Context, workstreams []models.Workstream) ([]*RouteResult, error) {
	r.mu.Lock()
	keys := make([]models.ConversationKey, 0, len(r.buffers))
	for key, buf := range r.buffers {
		if len(buf.signals) > 0 {
			keys = append(keys, key)
		}
	}
	r.mu.Unlock()

	var results []*RouteResult
	var errs []error
	for _, key := range keys {
		r.mu.Lock()
		buf := r.buffers[key]
		if buf == nil || len(buf.signals) == 0 {
			r.mu.Unlock()
			continue
		}
		signals := buf.signals
		buf.signals = nil

		var currentAffinityIDs []string
		if entries, ok := r.affinities[key]; ok {
			for _, e := range entries {
				currentAffinityIDs = append(currentAffinityIDs, e.WorkstreamID)
			}
		}
		r.stats.ClassifierCalls++
		r.mu.Unlock()

		result, err := r.classifier.Classify(ctx, key, signals, workstreams, currentAffinityIDs)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		wsIDs := result.WorkstreamIDs
		if len(wsIDs) == 0 && result.NewWorkstreamName == "" {
			r.mu.Lock()
			wsIDs = []string{models.DefaultWorkstreamID(r.workspace)}
			r.mu.Unlock()
		}

		rr := &RouteResult{
			Decision: models.RoutingDecision{
				SignalID:      signals[0].ID,
				WorkstreamIDs: wsIDs,
				Ts:            signals[len(signals)-1].Ts,
			},
			NewWorkstreamName:  result.NewWorkstreamName,
			NewWorkstreamFocus: result.NewWorkstreamFocus,
		}

		r.logger.Info("flush classified",
			"conversation", key.Conversation,
			"signals", len(signals),
			"workstreams", wsIDs,
			"new_workstream", result.NewWorkstreamName,
		)

		results = append(results, rr)
	}

	return results, nil
}

// Stats holds routing statistics.
type Stats struct {
	FastPathRouted  int
	ClassifierCalls int
	BufferedSignals int
}

// Stats returns routing statistics.
func (r *Router) Stats() Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stats
}
