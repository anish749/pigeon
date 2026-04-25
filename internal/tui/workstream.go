// Package tui — workstream management TUI.
//
// This is a separate Bubble Tea program from the outbox review TUI in
// review.go. It manages workstream lifecycle (rename, edit focus, change
// state, merge, split, delete) for a single workspace, talking directly to
// the workstream FS store.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/store"
)

var (
	wsTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	wsSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	wsActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	wsDormantStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	wsResolvedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	wsDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	wsErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	wsHelpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	wsHintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("219"))
	wsBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// RunWorkstream starts the workstream-management TUI for a workspace.
func RunWorkstream(st store.Store, ws config.WorkspaceName) error {
	m := wsModel{
		store:     st,
		workspace: ws,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

type wsMode int

const (
	wsModeList wsMode = iota
	wsModeEditName
	wsModeEditFocus
	wsModeNewName
	wsModeNewFocus
	wsModeMergePick
	wsModeConfirmDelete
)

type wsModel struct {
	store     store.Store
	workspace config.WorkspaceName

	items  []models.Workstream
	cursor int

	mode   wsMode
	input  string
	scratchName string // for split/new flow: holds name while collecting focus
	mergeCursor int   // index into items for merge target
	status string
	err    error

	width  int
	height int
}

type wsLoadedMsg struct {
	items []models.Workstream
	err   error
}

type wsClearStatusMsg struct{}

func (m wsModel) Init() tea.Cmd {
	return m.load()
}

func (m wsModel) load() tea.Cmd {
	return func() tea.Msg {
		all, err := m.store.ListWorkstreams()
		if err != nil {
			return wsLoadedMsg{err: err}
		}
		// Filter to this workspace, sort default last, then by state then name.
		var filtered []models.Workstream
		for _, w := range all {
			if w.Workspace == m.workspace || (m.workspace == "" && w.Workspace == "") {
				filtered = append(filtered, w)
			}
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].IsDefault() != filtered[j].IsDefault() {
				return !filtered[i].IsDefault()
			}
			if filtered[i].State != filtered[j].State {
				return stateRank(filtered[i].State) < stateRank(filtered[j].State)
			}
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		})
		return wsLoadedMsg{items: filtered}
	}
}

func stateRank(s models.WorkstreamState) int {
	switch s {
	case models.StateActive:
		return 0
	case models.StateDormant:
		return 1
	case models.StateResolved:
		return 2
	}
	return 3
}

func (m wsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case wsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.items = msg.items
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		m.err = nil
	case wsClearStatusMsg:
		m.status = ""
	case wsStatusMsg:
		m.status = string(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m wsModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case wsModeEditName, wsModeEditFocus, wsModeNewName, wsModeNewFocus:
		return m.handleInputKey(msg)
	case wsModeMergePick:
		return m.handleMergeKey(msg)
	case wsModeConfirmDelete:
		return m.handleConfirmKey(msg)
	}

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
			m.mode = wsModeEditName
			m.input = w.Name
		}
	case "e":
		if w, ok := m.current(); ok {
			m.mode = wsModeEditFocus
			m.input = w.Focus
		}
	case "s":
		if w, ok := m.current(); ok && !w.IsDefault() {
			return m, m.cycleState(w)
		}
	case "m":
		if w, ok := m.current(); ok && !w.IsDefault() && len(m.items) > 1 {
			m.mode = wsModeMergePick
			m.mergeCursor = 0
			if m.mergeCursor == m.cursor {
				m.mergeCursor = (m.cursor + 1) % len(m.items)
			}
		}
	case "n":
		m.mode = wsModeNewName
		m.input = ""
		m.scratchName = ""
	case "d":
		if w, ok := m.current(); ok && !w.IsDefault() {
			m.mode = wsModeConfirmDelete
		}
	}
	return m, nil
}

func (m wsModel) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := tea.Key(msg)
	switch k.Type {
	case tea.KeyEscape:
		m.mode = wsModeList
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

func (m wsModel) commitInput() (tea.Model, tea.Cmd) {
	switch m.mode {
	case wsModeEditName:
		w, ok := m.current()
		if !ok || strings.TrimSpace(m.input) == "" {
			m.mode = wsModeList
			return m, nil
		}
		w.Name = strings.TrimSpace(m.input)
		m.mode = wsModeList
		m.input = ""
		return m, m.put(w, "renamed")
	case wsModeEditFocus:
		w, ok := m.current()
		if !ok {
			m.mode = wsModeList
			return m, nil
		}
		w = w.WithFocus(strings.TrimSpace(m.input))
		m.mode = wsModeList
		m.input = ""
		return m, m.put(w, "focus updated")
	case wsModeNewName:
		name := strings.TrimSpace(m.input)
		if name == "" {
			m.mode = wsModeList
			return m, nil
		}
		m.scratchName = name
		m.input = ""
		m.mode = wsModeNewFocus
	case wsModeNewFocus:
		focus := strings.TrimSpace(m.input)
		name := m.scratchName
		m.input = ""
		m.scratchName = ""
		m.mode = wsModeList
		if name == "" {
			return m, nil
		}
		w := models.Workstream{
			ID:        "ws-" + slug.Make(name),
			Name:      name,
			Workspace: m.workspace,
			State:     models.StateActive,
			Focus:     focus,
			Created:   time.Now().UTC(),
		}
		return m, m.put(w, "created")
	}
	return m, nil
}

func (m wsModel) handleMergeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = wsModeList
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
		src, ok1 := m.current()
		if !ok1 || m.mergeCursor < 0 || m.mergeCursor >= len(m.items) || m.mergeCursor == m.cursor {
			m.mode = wsModeList
			return m, nil
		}
		dst := m.items[m.mergeCursor]
		m.mode = wsModeList
		return m, m.merge(src, dst)
	}
	return m, nil
}

func (m wsModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		w, ok := m.current()
		if !ok {
			m.mode = wsModeList
			return m, nil
		}
		m.mode = wsModeList
		return m, m.delete(w)
	case "n", "N", "esc", "q":
		m.mode = wsModeList
	}
	return m, nil
}

func (m wsModel) current() (models.Workstream, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return models.Workstream{}, false
	}
	return m.items[m.cursor], true
}

// --- mutations ---

func (m wsModel) put(w models.Workstream, detail string) tea.Cmd {
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.PutWorkstream(w); err != nil {
				return wsLoadedMsg{err: fmt.Errorf("save: %w", err)}
			}
			return nil
		},
		setStatus(detail),
		m.load(),
	)
}

func (m wsModel) cycleState(w models.Workstream) tea.Cmd {
	next := models.StateActive
	switch w.State {
	case models.StateActive:
		next = models.StateDormant
	case models.StateDormant:
		next = models.StateResolved
	case models.StateResolved:
		next = models.StateActive
	}
	w = w.WithState(next)
	return m.put(w, fmt.Sprintf("%s → %s", w.Name, next))
}

func (m wsModel) merge(src, dst models.Workstream) tea.Cmd {
	combinedFocus := dst.Focus
	if strings.TrimSpace(src.Focus) != "" && !strings.Contains(combinedFocus, src.Focus) {
		combinedFocus = strings.TrimSpace(combinedFocus + "\n\n[merged from " + src.Name + "] " + src.Focus)
	}
	dst = dst.WithFocus(combinedFocus)
	src = src.WithState(models.StateResolved)
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.PutWorkstream(dst); err != nil {
				return wsLoadedMsg{err: fmt.Errorf("merge target: %w", err)}
			}
			if err := st.PutWorkstream(src); err != nil {
				return wsLoadedMsg{err: fmt.Errorf("merge source: %w", err)}
			}
			return nil
		},
		setStatus(fmt.Sprintf("merged %s → %s", src.Name, dst.Name)),
		m.load(),
	)
}

func (m wsModel) delete(w models.Workstream) tea.Cmd {
	st := m.store
	return tea.Batch(
		func() tea.Msg {
			if err := st.DeleteWorkstream(w.ID); err != nil {
				return wsLoadedMsg{err: fmt.Errorf("delete: %w", err)}
			}
			return nil
		},
		setStatus("deleted "+w.Name),
		m.load(),
	)
}

func setStatus(s string) tea.Cmd {
	return tea.Sequence(
		func() tea.Msg { return wsStatusMsg(s) },
		tea.Tick(3*time.Second, func(time.Time) tea.Msg { return wsClearStatusMsg{} }),
	)
}

type wsStatusMsg string

// --- view ---

func (m wsModel) View() string {
	var b strings.Builder
	header := fmt.Sprintf("  Pigeon Workstreams  %s", wsDimStyle.Render(string(m.workspace)))
	b.WriteString(wsTitleStyle.Render(header))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(wsErrorStyle.Render(fmt.Sprintf("  Error: %v\n", m.err)))
	}

	if len(m.items) == 0 {
		b.WriteString(wsDimStyle.Render("  No workstreams in this workspace.\n"))
		b.WriteString(wsDimStyle.Render("  Press n to create one, or run 'pigeon workstream discover'.\n"))
		b.WriteString("\n")
		b.WriteString(wsHelpStyle.Render("  n new   q quit"))
		return b.String()
	}

	for i, w := range m.items {
		marker := "  "
		name := w.Name
		if i == m.cursor {
			marker = wsSelectedStyle.Render("● ")
			name = wsSelectedStyle.Render(name)
		} else {
			name = wsDimStyle.Render(name)
		}
		state := renderState(w.State)
		def := ""
		if w.IsDefault() {
			def = wsDimStyle.Render(" (default)")
		}
		b.WriteString(fmt.Sprintf("%s%s  %s%s\n", marker, state, name, def))
	}
	b.WriteString("\n")

	if w, ok := m.current(); ok {
		b.WriteString(m.renderDetail(w))
		b.WriteString("\n")
	}

	if m.status != "" {
		b.WriteString("  " + wsHintStyle.Render(m.status) + "\n\n")
	}

	switch m.mode {
	case wsModeEditName:
		b.WriteString("  " + wsTitleStyle.Render("Rename:") + " " + m.input + "█\n")
		b.WriteString(wsHelpStyle.Render("  enter save  esc cancel"))
	case wsModeEditFocus:
		b.WriteString("  " + wsTitleStyle.Render("Edit focus:") + " " + m.input + "█\n")
		b.WriteString(wsHelpStyle.Render("  enter save  esc cancel"))
	case wsModeNewName:
		b.WriteString("  " + wsTitleStyle.Render("New workstream — name:") + " " + m.input + "█\n")
		b.WriteString(wsHelpStyle.Render("  enter next  esc cancel"))
	case wsModeNewFocus:
		b.WriteString("  " + wsTitleStyle.Render("New workstream — focus:") + " " + m.input + "█\n")
		b.WriteString(wsHelpStyle.Render("  enter create  esc cancel"))
	case wsModeMergePick:
		b.WriteString(m.renderMergePicker())
	case wsModeConfirmDelete:
		w, _ := m.current()
		b.WriteString("  " + wsErrorStyle.Render(fmt.Sprintf("Delete %q? (y/n)", w.Name)))
	default:
		help := "  r rename  e edit focus  s state  m merge  n new  d delete  j/k nav  q quit"
		if w, ok := m.current(); ok && w.IsDefault() {
			help = "  e edit focus  n new  j/k nav  q quit  " + wsDimStyle.Render("(default — limited actions)")
		}
		b.WriteString(wsHelpStyle.Render(help))
	}
	return b.String()
}

func (m wsModel) renderDetail(w models.Workstream) string {
	maxWidth := m.width - 6
	if maxWidth < 40 {
		maxWidth = 40
	}
	body := fmt.Sprintf("Focus: %s\nID: %s\nCreated: %s",
		emptyOr(w.Focus, "(no focus set)"),
		w.ID,
		w.Created.Format("2006-01-02"))
	box := wsBoxStyle.Width(maxWidth).Render(body)
	var b strings.Builder
	for _, line := range strings.Split(box, "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

func (m wsModel) renderMergePicker() string {
	src, _ := m.current()
	var b strings.Builder
	b.WriteString("  " + wsTitleStyle.Render(fmt.Sprintf("Merge %q into:", src.Name)) + "\n")
	for i, w := range m.items {
		if i == m.cursor {
			continue
		}
		marker := "    "
		name := w.Name
		if i == m.mergeCursor {
			marker = "  " + wsSelectedStyle.Render("→ ")
			name = wsSelectedStyle.Render(name)
		} else {
			name = wsDimStyle.Render(name)
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n", marker, renderState(w.State), name))
	}
	b.WriteString("\n")
	b.WriteString(wsHelpStyle.Render("  enter confirm  j/k pick  esc cancel"))
	return b.String()
}

func renderState(s models.WorkstreamState) string {
	switch s {
	case models.StateActive:
		return wsActiveStyle.Render("●active  ")
	case models.StateDormant:
		return wsDormantStyle.Render("◌dormant ")
	case models.StateResolved:
		return wsResolvedStyle.Render("✓resolved")
	}
	return wsDimStyle.Render("?        ")
}

func emptyOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
