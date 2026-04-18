package models

import "time"

// WorkstreamState represents the lifecycle state of a workstream.
type WorkstreamState string

const (
	StateActive   WorkstreamState = "active"
	StateDormant  WorkstreamState = "dormant"
	StateResolved WorkstreamState = "resolved"
)

// Workstream represents a coherent ongoing effort that accumulates signals.
type Workstream struct {
	ID        string          // unique identifier (e.g. "ws-recommendations")
	Name      string          // human-readable name (e.g. "Recommendations")
	Workspace string          // which workspace this belongs to (e.g. "acme")
	State     WorkstreamState // active, dormant, resolved

	// Focus is the LLM-generated description of what this workstream is about.
	// Used by the batch classifier for routing comparisons. Gets periodically
	// refreshed by the workstream manager as signals accumulate.
	Focus string

	// Participants lists display names of people involved.
	Participants []string

	// Entities are extracted anchors (PR numbers, ticket IDs, service names)
	// used for quick entity-matching before falling back to semantic classification.
	Entities []string

	// Signal tracking
	SignalCount int       // total signals routed here
	Created     time.Time // when this workstream was first created
	LastSignal  time.Time // timestamp of most recent signal
}

// DefaultWorkstreamID returns the ID for a workspace's default "just chatting" workstream.
func DefaultWorkstreamID(workspace string) string {
	return "_default_" + workspace
}

// NewDefaultWorkstream creates the default catch-all workstream for a workspace.
func NewDefaultWorkstream(workspace string) *Workstream {
	return &Workstream{
		ID:        DefaultWorkstreamID(workspace),
		Name:      "General",
		Workspace: workspace,
		State:     StateActive,
		Focus:     "Unclassified signals — general conversation, one-off questions, coordination that doesn't belong to a specific workstream yet.",
		Created:   time.Now(),
	}
}

// IsDefault reports whether this is the workspace default workstream.
func (w *Workstream) IsDefault() bool {
	return w.ID == DefaultWorkstreamID(w.Workspace)
}
