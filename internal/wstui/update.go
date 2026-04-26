package wstui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/workstream/models"
)

// Init kicks off the initial store load.
func (m Model) Init() tea.Cmd {
	return loadCmd(m)
}

// Update is the bubble tea message dispatcher. It is intentionally
// thin — each branch returns a (Model, Cmd) tuple from a small
// dedicated function so individual transitions are easy to test.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case loadedMsg:
		return m.applyLoaded(msg), nil
	case statusMsg:
		m.status = string(msg)
	case clearStatusMsg:
		m.status = ""
	case spinTickMsg:
		// The ticker only matters while discovery is in flight. Once the
		// goroutine posts discoverDoneMsg and we leave modeDiscovering,
		// we stop scheduling the next tick — late ticks are dropped here.
		if m.mode == modeDiscovering {
			m.spinnerFrame++
			return m, spinTick()
		}
	case discoverDoneMsg:
		return m.applyDiscoverDone(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyDiscoverDone exits modeDiscovering and either flashes a success
// status (and reloads the list) or surfaces the error.
func (m Model) applyDiscoverDone(msg discoverDoneMsg) (tea.Model, tea.Cmd) {
	m.mode = modeList
	m.spinnerFrame = 0
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	detail := fmt.Sprintf("discovered %d workstreams", msg.count)
	if msg.count == 0 {
		detail = "no workstreams found"
	}
	return m, tea.Batch(setStatus(detail), loadCmd(m))
}

// applyLoaded swaps in fresh items and clamps the cursor.
func (m Model) applyLoaded(msg loadedMsg) Model {
	if msg.err != nil {
		m.err = msg.err
		return m
	}
	m.items = msg.items
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.err = nil
	return m
}

// handleKey routes a key press to the right per-mode handler. Ctrl+C
// always quits regardless of mode — without this, sub-modes like
// modeMergePick or any input mode would trap the user with no way out
// because their handlers don't otherwise dispatch tea.Quit.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeEditName, modeEditFocus, modeNewName, modeNewFocus:
		return m.handleInputKey(msg)
	case modeMergePick:
		return m.handleMergeKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeDiscovering:
		// Discovery runs to completion (or context timeout). All keys
		// other than Ctrl+C above are ignored — re-pressing D would
		// double-fire the LLM call.
		return m, nil
	}
	return m.handleListKey(msg)
}

// handleListKey covers the default browse mode keys.
func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "r":
		if w, ok := m.current(); ok && !w.IsDefault() {
			m.mode = modeEditName
			m.input = w.Name
		}
	case "e":
		if w, ok := m.current(); ok {
			m.mode = modeEditFocus
			m.input = w.Focus
		}
	case "s":
		if w, ok := m.current(); ok && !w.IsDefault() {
			return m, cycleStateCmd(m, w)
		}
	case "m":
		if w, ok := m.current(); ok && !w.IsDefault() && len(m.items) > 1 {
			m.mode = modeMergePick
			m.mergeCursor = firstMergeTarget(m.cursor, len(m.items))
			_ = w
		}
	case "n":
		m.mode = modeNewName
		m.input = ""
		m.scratchName = ""
	case "d":
		if w, ok := m.current(); ok && !w.IsDefault() {
			m.mode = modeConfirmDelete
			_ = w
		}
	case "D":
		if m.discoverFn != nil {
			m.mode = modeDiscovering
			m.spinnerFrame = 0
			m.err = nil
			return m, discoverCmd(m.discoverFn)
		}
	}
	return m, nil
}

// firstMergeTarget picks an initial cursor for the merge picker that
// avoids the source row.
func firstMergeTarget(cursor, n int) int {
	if cursor == 0 && n > 1 {
		return 1
	}
	return 0
}

// handleInputKey accumulates characters while in any input-collecting
// mode. Enter commits via commitInput; Esc cancels.
func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := tea.Key(msg)
	switch k.Type {
	case tea.KeyEscape:
		m.mode = modeList
		m.input = ""
		m.scratchName = ""
	case tea.KeyEnter:
		return m.commitInput()
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case tea.KeyRunes, tea.KeySpace:
		m.input += string(k.Runes)
	}
	return m, nil
}

// commitInput dispatches the in-flight input to the right action based
// on the current mode and clears scratch state.
func (m Model) commitInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeEditName:
		return m.commitEditName()
	case modeEditFocus:
		return m.commitEditFocus()
	case modeNewName:
		return m.commitNewName()
	case modeNewFocus:
		return m.commitNewFocus()
	}
	return m, nil
}

func (m Model) commitEditName() (tea.Model, tea.Cmd) {
	w, ok := m.current()
	name := strings.TrimSpace(m.input)
	m.mode = modeList
	m.input = ""
	if !ok || name == "" {
		return m, nil
	}
	w.Name = name
	return m, putCmd(m, w, "renamed")
}

func (m Model) commitEditFocus() (tea.Model, tea.Cmd) {
	w, ok := m.current()
	focus := strings.TrimSpace(m.input)
	m.mode = modeList
	m.input = ""
	if !ok {
		return m, nil
	}
	return m, putCmd(m, w.WithFocus(focus), "focus updated")
}

func (m Model) commitNewName() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.input)
	m.input = ""
	if name == "" {
		m.mode = modeList
		m.scratchName = ""
		return m, nil
	}
	m.scratchName = name
	m.mode = modeNewFocus
	return m, nil
}

func (m Model) commitNewFocus() (tea.Model, tea.Cmd) {
	focus := strings.TrimSpace(m.input)
	name := m.scratchName
	m.input = ""
	m.scratchName = ""
	m.mode = modeList
	if name == "" {
		return m, nil
	}
	w := models.NewWorkstream(name, m.workspace, focus, time.Now().UTC())
	return m, putCmd(m, w, "created")
}

// handleMergeKey runs while the merge target picker is open.
func (m Model) handleMergeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeList
	case "j", "down":
		next := m.mergeCursor + 1
		if next == m.cursor {
			next++
		}
		if next < len(m.items) {
			m.mergeCursor = next
		}
	case "k", "up":
		next := m.mergeCursor - 1
		if next == m.cursor {
			next--
		}
		if next >= 0 {
			m.mergeCursor = next
		}
	case "enter":
		src, ok := m.current()
		if !ok || m.mergeCursor < 0 || m.mergeCursor >= len(m.items) || m.mergeCursor == m.cursor {
			m.mode = modeList
			return m, nil
		}
		dst := m.items[m.mergeCursor]
		m.mode = modeList
		return m, mergeCmd(m, src, dst)
	}
	return m, nil
}

// handleConfirmKey runs while the delete confirmation prompt is open.
func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		w, ok := m.current()
		m.mode = modeList
		if !ok {
			return m, nil
		}
		return m, deleteCmd(m, w)
	case "n", "N", "esc", "q":
		m.mode = modeList
	}
	return m, nil
}
