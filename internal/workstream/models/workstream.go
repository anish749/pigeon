package models

import (
	"time"

	"github.com/anish749/pigeon/internal/config"
)

// Workstream is an immutable definition of a coherent ongoing effort.
// It carries no counters or derived stats — those are computed from the
// RoutingLedger. Mutations produce a new Workstream value via the With*
// methods.
type Workstream struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Workspace config.WorkspaceName `json:"workspace"`
	Focus     string               `json:"focus"`
	Created   time.Time            `json:"created"`
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
