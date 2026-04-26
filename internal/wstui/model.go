package wstui

import (
	"context"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

// DiscoverFunc runs LLM-based workstream discovery for the model's
// workspace and persists the result to the same store the TUI reads
// from. It returns the number of workstreams discovered. The TUI calls
// it in a goroutine and renders a spinner until it returns.
//
// Implementations should treat ctx cancellation as the user aborting —
// the store should not be left in a partial state on cancel.
type DiscoverFunc func(ctx context.Context) (int, error)

// mode is the input-handling state of the TUI. Most modes consume keys
// for inline editing; modeList is the default browse mode.
type mode int

const (
	modeList mode = iota
	modeEditName
	modeEditFocus
	modeNewName
	modeNewFocus
	modeMergePick
	modeConfirmDelete
	modeDiscovering
)

// Model is the bubble-tea model for the workstream TUI. Exported only
// because Bubble Tea requires it; callers should use Run.
type Model struct {
	store      store.Store
	workspace  config.WorkspaceName
	discoverFn DiscoverFunc // optional; nil disables in-app discovery

	items  []models.Workstream
	cursor int

	mode        mode
	input       string
	scratchName string // holds name across the new-name → new-focus transition
	mergeCursor int    // selected target while in modeMergePick

	// spinnerFrame advances on each spinTickMsg while in modeDiscovering.
	// Non-zero only during discovery.
	spinnerFrame int

	status string // transient status line, cleared after a delay
	err    error  // sticky error; cleared on next successful load

	width  int
	height int
}

// NewModel returns a model bound to st and scoped to ws. Used by Run
// and by tests. discover may be nil; when set, the 'D' key and the
// empty-state prompt expose in-app discovery.
func NewModel(st store.Store, ws config.WorkspaceName, discover DiscoverFunc) Model {
	return Model{store: st, workspace: ws, discoverFn: discover}
}

// current returns the workstream under the cursor, or zero+false if the
// list is empty or the cursor is out of range.
func (m Model) current() (models.Workstream, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return models.Workstream{}, false
	}
	return m.items[m.cursor], true
}

// --- bubble tea messages ---

// loadedMsg is dispatched after the store is read, populating items.
type loadedMsg struct {
	items []models.Workstream
	err   error
}

// statusMsg sets the transient status line.
type statusMsg string

// clearStatusMsg clears the transient status line.
type clearStatusMsg struct{}

// spinTickMsg advances the discovery-mode spinner frame.
type spinTickMsg struct{}

// discoverDoneMsg is dispatched when the DiscoverFunc goroutine
// returns. count is the number of workstreams produced; err is any
// error from the discovery pipeline (or context cancellation).
type discoverDoneMsg struct {
	count int
	err   error
}
