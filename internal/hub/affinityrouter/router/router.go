// Package router routes signals to workstreams using conversation affinity
// as the fast path and batch classification as the slow path.
//
// The Router is read-only during routing — it never writes to the ledger or
// creates workstreams. The caller (replay/orchestrator) is responsible for
// recording decisions and acting on new-workstream proposals.
package router

import (
	"context"
	"fmt"
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

	// Internal state.
	affinities  map[models.ConversationKey][]models.AffinityEntry              // conversation → workstream weights
	detectors   map[models.ConversationKey]detector.ConversationShiftDetector   // per-conversation shift detectors
	classifiers map[models.ConversationKey]classifier.WorkstreamClassifier      // per-conversation classifiers

	// Config.
	workspace config.WorkspaceName

	// Stats.
	stats Stats

	mu sync.RWMutex
}

// New creates a Router with the given shift detector and classifier factories.
// If st is non-nil, affinities are loaded from it on creation and persisted
// after each UpdateAffinity call.
func New(detectorFactory detector.Factory, classifierFactory classifier.Factory, cfg models.Config, st store.Store, logger *slog.Logger) (*Router, error) {
	affinities := make(map[models.ConversationKey][]models.AffinityEntry)
	if st != nil {
		loaded, err := st.LoadAffinities()
		if err != nil {
			return nil, fmt.Errorf("load affinities: %w", err)
		}
		if loaded != nil {
			affinities = loaded
		}
	}
	return &Router{
		newDetector:   detectorFactory,
		newClassifier: classifierFactory,
		store:         st,
		logger:        logger,
		affinities:    affinities,
		detectors:     make(map[models.ConversationKey]detector.ConversationShiftDetector),
		classifiers:   make(map[models.ConversationKey]classifier.WorkstreamClassifier),
		workspace:     cfg.Workspace.Name,
	}, nil
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
	if entries, ok := r.affinities[key]; ok && len(entries) > 0 {
		affinityIDs = make([]string, len(entries))
		for i, e := range entries {
			affinityIDs[i] = e.WorkstreamID
		}
		r.stats.FastPathRouted++
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
		// No shift detected — observe the signal with the router's decision.
		cls.Observe(sig, decision)
		r.stats.BufferedSignals++
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

	r.logger.Info("classified batch",
		"account", key.Account.Display(),
		"conversation", key.Conversation,
		"window", cls.Buffered(),
		"reclassified", len(result.Routings),
		"new_workstream", result.NewWorkstreamName,
	)

	return routeResult, nil
}

// UpdateAffinity updates the conversation->workstream affinity weights.
// Called by the orchestrator after recording a routing decision in the ledger.
func (r *Router) UpdateAffinity(key models.ConversationKey, workstreamID string, ts time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries := r.affinities[key]
	for i, e := range entries {
		if e.WorkstreamID == workstreamID {
			entries[i].Strength++
			entries[i].LastSignal = ts
			r.affinities[key] = entries
			return r.persistAffinities()
		}
	}
	r.affinities[key] = append(entries, models.AffinityEntry{
		WorkstreamID: workstreamID,
		Strength:     1,
		LastSignal:   ts,
	})
	return r.persistAffinities()
}

// persistAffinities saves affinities to the store. Must be called with mu held.
func (r *Router) persistAffinities() error {
	if r.store == nil {
		return nil
	}
	return r.store.SaveAffinities(r.affinities)
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
