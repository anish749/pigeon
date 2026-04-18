package models

import (
	"time"

	"github.com/anish749/pigeon/internal/config"
)

// WorkstreamState represents the lifecycle state of a workstream.
type WorkstreamState string

const (
	StateActive   WorkstreamState = "active"
	StateDormant  WorkstreamState = "dormant"
	StateResolved WorkstreamState = "resolved"
)

// Workstream is an immutable definition of a coherent ongoing effort.
// It carries no counters or derived stats — those are computed from the
// RoutingLedger. State changes (focus update, dormancy) produce a new
// Workstream value via the With* methods.
type Workstream struct {
	ID        string               // unique identifier
	Name      string               // human-readable name
	Workspace config.WorkspaceName // which workspace this belongs to
	State     WorkstreamState      // active, dormant, resolved
	Focus     string               // LLM-generated description, used for routing
	Created   time.Time            // when this workstream was first created
}

// DefaultWorkstreamID returns the ID for a workspace's default workstream.
func DefaultWorkstreamID(ws config.WorkspaceName) string {
	return "_default_" + string(ws)
}

// NewDefaultWorkstream creates the default catch-all workstream for a workspace.
// The ts parameter should be the timestamp of the first signal in this workspace.
func NewDefaultWorkstream(ws config.WorkspaceName, ts time.Time) Workstream {
	return Workstream{
		ID:        DefaultWorkstreamID(ws),
		Name:      "General",
		Workspace: ws,
		State:     StateActive,
		Focus:     "Unclassified signals — general conversation, coordination that doesn't belong to a specific workstream.",
		Created:   ts,
	}
}

// IsDefault reports whether this is the workspace default workstream.
func (w Workstream) IsDefault() bool {
	return w.ID == DefaultWorkstreamID(w.Workspace)
}

// WithFocus returns a copy with an updated focus description.
func (w Workstream) WithFocus(focus string) Workstream {
	w.Focus = focus
	return w
}

// WithState returns a copy with an updated state.
func (w Workstream) WithState(state WorkstreamState) Workstream {
	w.State = state
	return w
}
