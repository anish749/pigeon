package wstui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/workstream/models"
)

// statusDuration is how long a transient status line stays visible
// before it's cleared automatically.
const statusDuration = 3 * time.Second

// loadCmd reads from the store and dispatches a loadedMsg containing
// the filtered+sorted slice for the model's workspace.
func loadCmd(m Model) tea.Cmd {
	return func() tea.Msg {
		all, err := m.store.ListWorkstreams()
		if err != nil {
			return loadedMsg{err: err}
		}
		return loadedMsg{items: filterAndSort(all, m.workspace)}
	}
}

// putCmd persists w and emits a status line and reload on success. Any
// store error is surfaced as an error-bearing loadedMsg so the model's
// existing error path renders it.
func putCmd(m Model, w models.Workstream, statusDetail string) tea.Cmd {
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.PutWorkstream(w); err != nil {
				return loadedMsg{err: fmt.Errorf("save %q: %w", w.Name, err)}
			}
			return nil
		},
		setStatus(statusDetail),
		loadCmd(m),
	)
}

// deleteCmd removes w by ID, then reloads.
func deleteCmd(m Model, w models.Workstream) tea.Cmd {
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.DeleteWorkstream(w.ID); err != nil {
				return loadedMsg{err: fmt.Errorf("delete %q: %w", w.Name, err)}
			}
			return nil
		},
		setStatus("deleted "+w.Name),
		loadCmd(m),
	)
}

// mergeCmd merges src into dst, persisting both, then reloads.
func mergeCmd(m Model, src, dst models.Workstream) tea.Cmd {
	mergedDst, retiredSrc := src.MergeInto(dst)
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.PutWorkstream(mergedDst); err != nil {
				return loadedMsg{err: fmt.Errorf("merge target %q: %w", mergedDst.Name, err)}
			}
			if err := st.PutWorkstream(retiredSrc); err != nil {
				return loadedMsg{err: fmt.Errorf("merge source %q: %w", retiredSrc.Name, err)}
			}
			return nil
		},
		setStatus(fmt.Sprintf("merged %s → %s", src.Name, dst.Name)),
		loadCmd(m),
	)
}

// cycleStateCmd advances w's state to the next in the active → dormant
// → resolved rotation and persists.
func cycleStateCmd(m Model, w models.Workstream) tea.Cmd {
	next := w.State.NextState()
	updated := w.WithState(next)
	return putCmd(m, updated, fmt.Sprintf("%s → %s", w.Name, next))
}

// setStatus emits a status line followed by an automatic clear after
// statusDuration.
func setStatus(s string) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return statusMsg(s) },
		tea.Tick(statusDuration, func(time.Time) tea.Msg { return clearStatusMsg{} }),
	)
}
