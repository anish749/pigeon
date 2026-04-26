// Package wstui implements the workstream management terminal UI used
// by `pigeon workstream tui`. It is a thin presentation layer on top of
// the workstream FS store: lifecycle operations live in
// internal/workstream/models, persistence in internal/workstream/store,
// and the package only owns rendering, keybindings, and dispatch.
//
// Files:
//   - wstui.go    — Run entry point.
//   - model.go    — Model struct, modes, message types.
//   - update.go   — Init/Update + per-mode key handlers.
//   - view.go     — View + render helpers.
//   - actions.go  — store mutations expressed as tea.Cmds.
//   - filter.go   — workspace-scope filtering and sorting.
//   - styles.go   — lipgloss style values.
package wstui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/store"
)

// Run starts the workstream-management TUI for ws, backed by st.
// Blocks until the user quits.
func Run(st store.Store, ws config.WorkspaceName) error {
	p := tea.NewProgram(NewModel(st, ws), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
