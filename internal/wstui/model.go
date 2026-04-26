package wstui

import (
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

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
)

// Model is the bubble-tea model for the workstream TUI. Exported only
// because Bubble Tea requires it; callers should use Run.
type Model struct {
	store     store.Store
	workspace config.WorkspaceName

	items  []models.Workstream
	cursor int

	mode        mode
	input       string
	scratchName string // holds name across the new-name → new-focus transition
	mergeCursor int    // selected target while in modeMergePick

	status string // transient status line, cleared after a delay
	err    error  // sticky error; cleared on next successful load

	width  int
	height int
}

// NewModel returns a model bound to st and scoped to ws. Used by Run
// and by tests.
func NewModel(st store.Store, ws config.WorkspaceName) Model {
	return Model{store: st, workspace: ws}
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
