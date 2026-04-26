package wstui

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/workstream/models"
)

const minDetailWidth = 40

// View renders the current model state. It branches on mode and the
// presence of items; helpers below cover each section.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	if m.err != nil {
		fmt.Fprintf(&b, "  %s\n", errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	if m.mode == modeDiscovering {
		b.WriteString(m.renderDiscovering())
		return m.renderFullScreen(b.String(), m.renderFooter())
	}

	if len(m.items) == 0 {
		b.WriteString(m.renderEmpty())
		return m.renderFullScreen(b.String(), m.renderFooter())
	}

	b.WriteString(m.renderList())
	b.WriteString("\n")

	if w, ok := m.current(); ok {
		b.WriteString(m.renderDetail(w))
		b.WriteString("\n")
	}

	if m.status != "" {
		fmt.Fprintf(&b, "  %s\n\n", hintStyle.Render(m.status))
	}

	return m.renderFullScreen(b.String(), m.renderFooter())
}

func (m Model) renderHeader() string {
	header := fmt.Sprintf("  Pigeon Workstreams  %s", dimStyle.Render(string(m.cfg.Workspace.Name)))
	return titleStyle.Render(header)
}

func (m Model) renderEmpty() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("  No workstreams in this workspace.\n\n"))
	if m.manager != nil {
		b.WriteString("  " + hintStyle.Render("Press D to discover workstreams from your messaging history,") + "\n")
		b.WriteString("  " + hintStyle.Render("or n to create one manually.") + "\n")
	} else {
		b.WriteString(dimStyle.Render("  Press n to create one.\n"))
	}
	return b.String()
}

// renderDiscovering replaces the list while a discovery call is in
// flight. The spinner is one of ten braille frames, advanced by
// spinTickMsg every ~120ms.
func (m Model) renderDiscovering() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[m.spinnerFrame%len(frames)]
	var b strings.Builder
	fmt.Fprintf(&b, "  %s  %s\n", hintStyle.Render(frame), titleStyle.Render("Discovering workstreams…"))
	b.WriteString(dimStyle.Render("  Reading signals and asking the LLM to identify ongoing efforts.\n"))
	b.WriteString(dimStyle.Render("  ctrl+c to abort.\n"))
	return b.String()
}

func (m Model) renderList() string {
	var b strings.Builder
	for i, w := range m.items {
		marker := "  "
		name := w.Name
		if i == m.cursor {
			marker = selectedStyle.Render("● ")
			name = selectedStyle.Render(name)
		} else {
			name = dimStyle.Render(name)
		}
		state := renderState(w.State)
		def := ""
		if w.IsDefault() {
			def = dimStyle.Render(" (default)")
		}
		fmt.Fprintf(&b, "%s%s  %s%s\n", marker, state, name, def)
	}
	return b.String()
}

func (m Model) renderDetail(w models.Workstream) string {
	width := m.width - 6
	if width < minDetailWidth {
		width = minDetailWidth
	}
	body := fmt.Sprintf("Focus: %s\nID: %s\nCreated: %s",
		emptyOr(w.Focus, "(no focus set)"),
		w.ID,
		w.Created.Format("2006-01-02"))
	box := boxStyle.Width(width).Render(body)

	var b strings.Builder
	for _, line := range strings.Split(box, "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

func (m Model) renderFooter() string {
	switch m.mode {
	case modeEditName:
		return inputPrompt("Rename:", m.input, "  enter save  esc cancel")
	case modeEditFocus:
		return inputPrompt("Edit focus:", m.input, "  enter save  esc cancel")
	case modeNewName:
		return inputPrompt("New workstream — name:", m.input, "  enter next  esc cancel")
	case modeNewFocus:
		return inputPrompt("New workstream — focus:", m.input, "  enter create  esc cancel")
	case modeMergePick:
		return m.renderMergePicker()
	case modeConfirmDelete:
		w, _ := m.current()
		return "  " + errorStyle.Render(fmt.Sprintf("Delete %q? (y/n)", w.Name))
	}
	return helpStyle.Render(listHelp(m))
}

func listHelp(m Model) string {
	if w, ok := m.current(); ok && w.IsDefault() {
		help := "  e edit focus  n new  j/k nav  q quit"
		if m.manager != nil {
			help = "  e edit focus  n new  D discover  j/k nav  q quit"
		}
		return help + "  " + dimStyle.Render("(default workstream: limited actions)")
	}
	help := "  r rename  e edit focus  s state  m merge  n new  d delete  j/k nav  q quit"
	if m.manager != nil {
		help = "  r rename  e edit focus  s state  m merge  n new  d delete  D discover  j/k nav  q quit"
	}
	return help
}

// inputPrompt renders an inline editor with a help hint underneath.
func inputPrompt(label, value, help string) string {
	return fmt.Sprintf("  %s %s█\n%s", titleStyle.Render(label), value, helpStyle.Render(help))
}

func (m Model) renderMergePicker() string {
	src, _ := m.current()
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n", titleStyle.Render(fmt.Sprintf("Merge %q into:", src.Name)))
	for i, w := range m.items {
		if i == m.cursor {
			continue
		}
		marker := "    "
		name := w.Name
		if i == m.mergeCursor {
			marker = "  " + selectedStyle.Render("→ ")
			name = selectedStyle.Render(name)
		} else {
			name = dimStyle.Render(name)
		}
		fmt.Fprintf(&b, "%s%s  %s\n", marker, renderState(w.State), name)
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  enter confirm  j/k pick  esc cancel"))
	return b.String()
}

// renderState returns the colored state badge used in the list and the
// merge picker.
func renderState(s models.WorkstreamState) string {
	switch s {
	case models.StateActive:
		return activeStyle.Render("●active  ")
	case models.StateDormant:
		return dormantStyle.Render("◌dormant ")
	case models.StateResolved:
		return resolvedStyle.Render("✓resolved")
	}
	return dimStyle.Render("?        ")
}

func emptyOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func (m Model) renderFullScreen(content, footer string) string {
	content = strings.TrimRight(content, "\n")
	footer = strings.TrimRight(footer, "\n")

	if m.height <= 0 {
		if footer == "" {
			return content
		}
		return content + "\n" + footer
	}

	contentLines := splitLines(content)
	footerLines := splitLines(footer)
	if footer == "" {
		footerLines = nil
	}
	if len(footerLines) > m.height {
		footerLines = footerLines[len(footerLines)-m.height:]
	}

	contentHeight := m.height - len(footerLines)
	if contentHeight < 0 {
		contentHeight = 0
	}
	if len(contentLines) > contentHeight {
		contentLines = contentLines[:contentHeight]
	}

	var b strings.Builder
	for _, line := range contentLines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for i := len(contentLines); i < contentHeight; i++ {
		b.WriteByte('\n')
	}
	for i, line := range footerLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
