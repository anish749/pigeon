package models

import (
	"strings"
	"time"

	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/config"
)

// NewWorkstream constructs an active workstream with an ID derived from
// the slugified name. Used by the TUI's "create new" flow and by callers
// that materialize discovery output into the store.
func NewWorkstream(name string, ws config.WorkspaceName, focus string, created time.Time) Workstream {
	return Workstream{
		ID:        "ws-" + slug.Make(name),
		Name:      name,
		Workspace: ws,
		State:     StateActive,
		Focus:     focus,
		Created:   created,
	}
}

// NextState returns the next state in the active → dormant → resolved → active
// rotation. Used by the TUI's state-cycle key.
func (s WorkstreamState) NextState() WorkstreamState {
	switch s {
	case StateActive:
		return StateDormant
	case StateDormant:
		return StateResolved
	case StateResolved:
		return StateActive
	}
	return StateActive
}

// MergeInto returns the result of merging this workstream into target.
// The returned target carries the combined focus (with a "[merged from
// X]" annotation when the source focus is non-empty and not already
// substring-contained); the returned source is marked Resolved.
//
// Callers should persist both returned values.
func (w Workstream) MergeInto(target Workstream) (mergedTarget, retiredSource Workstream) {
	combined := target.Focus
	srcFocus := strings.TrimSpace(w.Focus)
	if srcFocus != "" && !strings.Contains(combined, srcFocus) {
		combined = strings.TrimSpace(combined + "\n\n[merged from " + w.Name + "] " + srcFocus)
	}
	return target.WithFocus(combined), w.WithState(StateResolved)
}
