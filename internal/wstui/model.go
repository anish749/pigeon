package wstui

import (
	"context"
	"time"

	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

// Manager is the surface the TUI consumes from
// internal/workstream/manager. Defined as a narrow interface so the
// TUI's import surface stays small and tests can fake just the methods
// in use. Grow this as more workstream lifecycle operations move into
// the TUI (ApproveProposal, RejectProposal, ProposeNew, etc.).
//
// *manager.Manager from internal/workstream/manager satisfies this
// interface — callers pass the same manager they built for the CLI
// `discover`/`replay` commands.
type Manager interface {
	DiscoverAndPropose(ctx context.Context, since, until time.Time) ([]discovery.DiscoveredWorkstream, error)
}

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
//
// The TUI consumes the same models.Config the manager was built with.
// Workspace scoping uses cfg.Workspace.Name; in-app discovery uses
// cfg.Since/cfg.Until. The TUI doesn't pick its own values — defaults
// (and any flag overrides) are the caller's responsibility.
type Model struct {
	store   store.Store
	cfg     models.Config
	manager Manager // optional; nil disables in-app discovery

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

// NewModel returns a model backed by st, configured by cfg, optionally
// wired to mgr for lifecycle operations. mgr may be nil; when set, the
// 'D' key and the empty-state prompt expose in-app discovery against
// cfg.Since/cfg.Until. Used by Run and by tests.
func NewModel(st store.Store, cfg models.Config, mgr Manager) Model {
	return Model{store: st, cfg: cfg, manager: mgr}
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

// discoverDoneMsg is dispatched when the in-flight discovery
// goroutine returns. count is the number of workstreams produced; err
// is any error from the discovery pipeline.
type discoverDoneMsg struct {
	count int
	err   error
}
