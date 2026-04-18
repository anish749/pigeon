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
	buffers    map[models.ConversationKey]*buffer                 // pending signals per conversation

	// Config.
	batchMinSignals int
	batchMaxAge     time.Duration

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
		batchMinSignals: cfg.BatchMinSignals,
		batchMaxAge:     cfg.BatchMaxAge,
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
		Workspace:    sig.Account.Name,
		Conversation: sig.Conversation,
	}
	now := time.Now()

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
				WorkstreamIDs: []string{models.DefaultWorkstreamID(key.Workspace)},
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

	routeResult := &RouteResult{
		Decision: models.RoutingDecision{
			SignalID:      sig.ID,
			WorkstreamIDs: result.WorkstreamIDs,
			Ts:            now,
		},
		NewWorkstreamName:  result.NewWorkstreamName,
		NewWorkstreamFocus: result.NewWorkstreamFocus,
	}

	r.logger.Info("classified batch",
		"workspace", key.Workspace,
		"conversation", key.Conversation,
		"signals", len(signals),
		"workstreams", result.WorkstreamIDs,
		"new_workstream", result.NewWorkstreamName,
	)

	return routeResult, nil
}

// bufferReady reports whether the buffer should be drained for classification.
func (r *Router) bufferReady(buf *buffer, now time.Time) bool {
	if len(buf.signals) == 0 {
		return false
	}

	// Enough signals accumulated.
	if len(buf.signals) >= r.batchMinSignals {
		return true
	}

	// Enough time since last classification.
	if !buf.lastClassified.IsZero() && now.Sub(buf.lastClassified) >= r.batchMaxAge {
		return true
	}

	// First batch: enough time since first signal.
	if buf.lastClassified.IsZero() && now.Sub(buf.signals[0].Ts) >= r.batchMaxAge {
		return true
	}

	return false
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
