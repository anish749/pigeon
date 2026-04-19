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
	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/store"
)

// Router routes signals to workstreams.
// It uses conversation affinity for the fast path and delegates to the
// classifier for batch classification when the detector triggers.
type Router struct {
	newDetector   detector.Factory
	newClassifier classifier.Factory
	store         store.Store
	logger        *slog.Logger

	// Ephemeral per-conversation state — not persisted.
	detectors   map[models.ConversationKey]detector.ConversationShiftDetector
	classifiers map[models.ConversationKey]classifier.WorkstreamClassifier

	// Config.
	workspace config.WorkspaceName

	// Stats.
	stats Stats

	mu sync.RWMutex
}

// New creates a Router with the given shift detector and classifier factories.
func New(detectorFactory detector.Factory, classifierFactory classifier.Factory, cfg models.Config, st store.Store, logger *slog.Logger) *Router {
	return &Router{
		newDetector:   detectorFactory,
		newClassifier: classifierFactory,
		store:         st,
		logger:        logger,
		detectors:     make(map[models.ConversationKey]detector.ConversationShiftDetector),
		classifiers:   make(map[models.ConversationKey]classifier.WorkstreamClassifier),
		workspace:     cfg.Workspace.Name,
	}
}

// RouteResult bundles the routing decision with classification results.
type RouteResult struct {
	// Decision is the routing decision for the current signal.
	Decision models.RoutingDecision

	// Reclassified contains signals whose workstream assignment changed
	// as a result of classification. Empty when the detector did not trigger.
	Reclassified []classifier.SignalRouting

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
// The signal is always sent to the classifier via Observe or
// ObserveAndClassify. When the detector triggers, ObserveAndClassify
// runs LLM classification and returns reclassification results.
//
// The workstreams parameter provides the current active workstreams for
// the signal's workspace (needed by the classifier).
//
// Returns one of:
//   - Decision with affinity-based workstream IDs (fast path, no classification)
//   - Decision with classifier-based workstream IDs (slow path, when detector triggers)
//   - Decision with default workstream ID (no affinity, detector not triggered)
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
	var affinityIDs []string
	entries, err := r.store.GetAffinities(key)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		affinityIDs = make([]string, len(entries))
		strengths := make([]int, len(entries))
		for i, e := range entries {
			affinityIDs[i] = e.WorkstreamID
			strengths[i] = e.Strength
		}
		r.stats.FastPathRouted++
		r.logger.Info("fast path: affinity hit",
			"account", sig.Account.Display(),
			"conversation", sig.Conversation,
			"workstreams", affinityIDs,
			"strengths", strengths,
			"sender", sig.Sender,
			"text", sig.Text,
		)
	} else {
		r.logger.Info("no affinity",
			"account", sig.Account.Display(),
			"conversation", sig.Conversation,
			"sender", sig.Sender,
			"text", sig.Text,
		)
	}

	// Get or create the detector and classifier for this conversation.
	det := r.detectors[key]
	if det == nil {
		det = r.newDetector()
		r.detectors[key] = det
	}
	cls := r.classifiers[key]
	if cls == nil {
		cls = r.newClassifier()
		r.classifiers[key] = cls
	}

	// Ask the detector whether this signal represents a shift.
	shifted := det.Observe(sig)

	// Determine the routing decision for this signal.
	wsIDs := affinityIDs
	if len(wsIDs) == 0 {
		wsIDs = []string{models.DefaultWorkstreamID(r.workspace)}
	}
	decision := models.RoutingDecision{
		SignalID:      sig.ID,
		WorkstreamIDs: wsIDs,
		Ts:            now,
	}

	if !shifted {
		cls.Observe(sig, decision)
		r.stats.BufferedSignals++
		r.logger.Info("no shift: buffered", "account", sig.Account.Display(), "conversation", sig.Conversation, "routed_to", decision.WorkstreamIDs, "buffered", cls.Buffered())
		return &RouteResult{Decision: decision}, nil
	}

	// Shift detected — observe and classify the full window.
	r.stats.ClassifierCalls++
	r.stats.BufferedSignals++

	// Release the lock during the classifier call (network/LLM).
	r.mu.Unlock()
	result, err := cls.ObserveAndClassify(ctx, sig, sig.Account, sig.Conversation, workstreams, affinityIDs)
	r.mu.Lock()

	if err != nil {
		r.stats.ClassifierCalls-- // don't count failed calls
		// On error, the signal was still observed by the classifier.
		// Fall back to the router's decision.
		return &RouteResult{Decision: decision}, err
	}

	// Use the classification result for the current signal's decision.
	if len(result.Routings) > 0 {
		// Find the current signal in the reclassified set.
		for _, routing := range result.Routings {
			if routing.Signal.ID == sig.ID {
				decision.WorkstreamIDs = routing.WorkstreamIDs
				break
			}
		}
	}

	routeResult := &RouteResult{
		Decision:           decision,
		Reclassified:       result.Routings,
		NewWorkstreamName:  result.NewWorkstreamName,
		NewWorkstreamFocus: result.NewWorkstreamFocus,
	}

	r.logger.Info("classified batch", "account", sig.Account.Display(), "conversation", key.Conversation, "window", cls.Buffered(), "reclassified", len(result.Routings), "new_workstream", result.NewWorkstreamName, "new_workstream_focus", result.NewWorkstreamFocus)

	return routeResult, nil
}

// UpdateAffinity updates the conversation->workstream affinity weights.
// Called by the orchestrator after recording a routing decision in the ledger.
func (r *Router) UpdateAffinity(key models.ConversationKey, workstreamID string, ts time.Time) error {
	entries, err := r.store.GetAffinities(key)
	if err != nil {
		return err
	}

	for i, e := range entries {
		if e.WorkstreamID == workstreamID {
			entries[i].Strength++
			entries[i].LastSignal = ts
			r.logger.Info("affinity strengthened", "conversation", key.Conversation, "workstream", workstreamID, "strength", entries[i].Strength)
			return r.store.PutAffinities(key, entries)
		}
	}
	entries = append(entries, models.AffinityEntry{
		WorkstreamID: workstreamID,
		Strength:     1,
		LastSignal:   ts,
	})
	r.logger.Info("affinity created", "conversation", key.Conversation, "workstream", workstreamID, "strength", 1)
	return r.store.PutAffinities(key, entries)
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
