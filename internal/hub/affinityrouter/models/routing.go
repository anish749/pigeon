package models

import "time"

// RoutingSource identifies how a routing decision was made.
type RoutingSource string

const (
	SourceAffinity   RoutingSource = "affinity"   // fast path — conversation history
	SourceClassifier RoutingSource = "classifier" // slow path — LLM classification
	SourceManual     RoutingSource = "manual"     // user explicitly routed
)

// RoutingDecision records the outcome of routing a signal to workstream(s).
// This is the single source of truth for "which signal went where."
// It is immutable — created once when the routing decision is made.
type RoutingDecision struct {
	SignalID      string        // which signal was routed
	WorkstreamIDs []string      // which workstream(s) it was routed to
	Source        RoutingSource // how the decision was made
	Confidence    float64       // 0-1, from the classifier (1.0 for affinity/manual)
	Ts            time.Time     // when the decision was made
}
