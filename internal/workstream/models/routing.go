package models

import "time"

// RoutingDecision records the outcome of routing a signal to workstream(s).
// This is the single source of truth for "which signal went where."
// It is immutable — created once when the routing decision is made.
type RoutingDecision struct {
	SignalID      string    `json:"signal_id"`
	WorkstreamIDs []string  `json:"workstream_ids"`
	Ts            time.Time `json:"ts"`
}
