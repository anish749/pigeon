package workstream

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anish749/pigeon/internal/workstream/models"
)

const (
	minDetailWidth = 40
	minListWidth   = 16
	maxListWidth   = 36
	bodyMargin     = 2 // spaces between terminal edge and content
	bodyGap        = 2 // spaces between list and detail columns
	defaultWidth   = 80
)

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

	if m.mode == modeMergePick {
		b.WriteString(indentLines(m.renderMergePicker(), strings.Repeat(" ", bodyMargin)))
		b.WriteString("\n")
	} else {
		leftWidth, rightWidth := m.columnWidths()
		listCol := m.renderListColumn(leftWidth)
		var rightCol string
		if w, ok := m.current(); ok {
			rightCol = m.renderDetailBox(w, rightWidth)
		}
		body := lipgloss.JoinHorizontal(lipgloss.Top, listCol, strings.Repeat(" ", bodyGap), rightCol)
		b.WriteString(indentLines(body, strings.Repeat(" ", bodyMargin)))
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

// columnWidths splits the body width between the list and the detail
// box. Returns sensible defaults when the terminal width hasn't been
// reported yet (initial render, tests).
func (m Model) columnWidths() (left, right int) {
	total := m.width
	if total <= 0 {
		total = defaultWidth
	}
	avail := total - 2*bodyMargin - bodyGap
	if avail < minListWidth+minDetailWidth {
		avail = minListWidth + minDetailWidth
	}
	left = avail / 3
	if left < minListWidth {
		left = minListWidth
	}
	if left > maxListWidth {
		left = maxListWidth
	}
	right = avail - left
	if right < minDetailWidth {
		right = minDetailWidth
	}
	return left, right
}

// renderListColumn renders the workstream list constrained to width.
// Each row consumes a 2-col marker zone (filled bullet for the cursor,
// hollow bullet otherwise) plus the wrapped name. Names that don't fit
// on one line wrap with a 2-col indent on continuations so the visual
// hierarchy stays clear. When the list is taller than the available
// height, only the slice that includes the cursor is rendered.
func (m Model) renderListColumn(width int) string {
	if width < minListWidth {
		width = minListWidth
	}
	const markerWidth = 2
	const continuationIndent = "  "
	nameWidth := width - markerWidth
	if nameWidth < 4 {
		nameWidth = 4
	}

	start, end := m.visibleRange(nameWidth)

	var b strings.Builder
	for i := start; i < end; i++ {
		w := m.items[i]
		var marker string
		var nameStyle lipgloss.Style
		if i == m.cursor {
			marker = selectedStyle.Render("● ")
			nameStyle = selectedStyle
		} else {
			marker = dimStyle.Render("○ ")
			nameStyle = dimStyle
		}
		lines := wrapName(w.Name, nameWidth, continuationIndent)
		for j, line := range lines {
			if j == 0 {
				b.WriteString(marker)
			} else {
				b.WriteString("  ")
			}
			b.WriteString(nameStyle.Render(line))
			if j == len(lines)-1 && w.IsDefault() {
				b.WriteString(dimStyle.Render(" (default)"))
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// visibleRange returns the [start, end) item indices for the workstream
// list given the current listOffset and cursor. Wraps the shared
// viewport helper with the per-item line counter that wraps names to
// nameWidth.
func (m Model) visibleRange(nameWidth int) (start, end int) {
	if nameWidth < 4 {
		nameWidth = 4
	}
	itemHeight := func(i int) int {
		return len(wrapName(m.items[i].Name, nameWidth, "  "))
	}
	return viewport(len(m.items), m.cursor, m.listOffset, m.listLineBudget(), itemHeight)
}

// listLineBudget is how many vertical lines the list column has to
// work with. Returns a large sentinel before the first WindowSizeMsg
// arrives (m.height == 0) so non-resizing tests render every item.
func (m Model) listLineBudget() int {
	if m.height <= 0 {
		return 1 << 30
	}
	reserved := 4 // title (1) + blank (1) + body trailing blank (1) + footer (1)
	if m.status != "" {
		reserved += 2
	}
	if m.err != nil {
		reserved++
	}
	budget := m.height - reserved
	if budget < 1 {
		return 1
	}
	return budget
}

// renderDetailBox renders the right-hand panel showing the selected
// workstream's name and focus, with reserved blank space at the bottom
// for future fields.
func (m Model) renderDetailBox(w models.Workstream, width int) string {
	if width < minDetailWidth {
		width = minDetailWidth
	}
	name := w.Name
	if w.IsDefault() {
		name += " (default)"
	}
	body := strings.Join([]string{
		titleStyle.Render(name),
		"",
		"Focus: " + emptyOr(w.Focus, "(no focus set)"),
		"",
		"",
	}, "\n")
	return boxStyle.Width(width).Render(body)
}

// wrapName breaks name into lines that each fit in width, indenting
// every line after the first by indent. A single word longer than
// width is allowed to overflow on its own line rather than being split
// mid-word.
func wrapName(name string, width int, indent string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{""}
	}
	if width <= 0 || lipgloss.Width(name) <= width {
		return []string{name}
	}
	words := strings.Fields(name)
	if len(words) == 0 {
		return []string{name}
	}
	indentW := lipgloss.Width(indent)
	var lines []string
	cur := words[0]
	curWidth := width
	for _, w := range words[1:] {
		candidate := cur + " " + w
		if lipgloss.Width(candidate) <= curWidth {
			cur = candidate
			continue
		}
		lines = append(lines, cur)
		cur = w
		curWidth = width - indentW
	}
	lines = append(lines, cur)
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	return lines
}

// indentLines prepends indent to every line of s.
func indentLines(s, indent string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
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
		return helpStyle.Render("  enter confirm  j/k pick  esc cancel")
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
	help := "  r rename  e edit focus  m merge  n new  d delete  j/k nav  q quit"
	if m.manager != nil {
		help = "  r rename  e edit focus  m merge  n new  d delete  D discover  j/k nav  q quit"
	}
	return help
}

// inputPrompt renders an inline editor with a help hint underneath.
func inputPrompt(label, value, help string) string {
	return fmt.Sprintf("  %s %s█\n%s", titleStyle.Render(label), value, helpStyle.Render(help))
}

// mergeCandidates returns the m.items indices that are valid merge
// targets — every workstream except the source row and the workspace
// default.
func (m Model) mergeCandidates() []int {
	var out []int
	for i, w := range m.items {
		if i == m.cursor || w.IsDefault() {
			continue
		}
		out = append(out, i)
	}
	return out
}

// mergeNameWidth is the per-row name area for the merge picker.
func (m Model) mergeNameWidth() int {
	width := m.width - 2*bodyMargin
	if width <= 0 {
		width = defaultWidth - 2*bodyMargin
	}
	const markerWidth = 4 // "  → " or four spaces
	nameWidth := width - markerWidth
	if nameWidth < 4 {
		nameWidth = 4
	}
	return nameWidth
}

// mergeViewport runs the shared viewport helper with the merge-picker's
// per-candidate height and budget.
func (m Model) mergeViewport(candidates []int, cursorPos, offsetPos int) (start, end int) {
	nameWidth := m.mergeNameWidth()
	itemHeight := func(k int) int {
		return len(wrapName(m.items[candidates[k]].Name, nameWidth, "  "))
	}
	return viewport(len(candidates), cursorPos, offsetPos, m.mergeLineBudget(), itemHeight)
}

func (m Model) renderMergePicker() string {
	src, _ := m.current()
	candidates := m.mergeCandidates()

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", titleStyle.Render(fmt.Sprintf("Merge %q into:", src.Name)))

	if len(candidates) == 0 {
		b.WriteString(dimStyle.Render("  (no eligible targets)"))
		return b.String()
	}

	cursorPos := indexOfInt(candidates, m.mergeCursor)
	if cursorPos < 0 {
		cursorPos = 0
	}
	offsetPos := indexOfInt(candidates, m.mergeOffset)
	if offsetPos < 0 {
		offsetPos = cursorPos
	}
	startK, endK := m.mergeViewport(candidates, cursorPos, offsetPos)
	nameWidth := m.mergeNameWidth()

	for k := startK; k < endK; k++ {
		idx := candidates[k]
		w := m.items[idx]
		var firstMarker string
		var nameStyle lipgloss.Style
		if idx == m.mergeCursor {
			firstMarker = "  " + selectedStyle.Render("→ ")
			nameStyle = selectedStyle
		} else {
			firstMarker = "    "
			nameStyle = dimStyle
		}
		lines := wrapName(w.Name, nameWidth, "  ")
		for j, line := range lines {
			if j == 0 {
				b.WriteString(firstMarker)
			} else {
				b.WriteString("    ")
			}
			b.WriteString(nameStyle.Render(line))
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergeLineBudget mirrors listLineBudget but reserves an extra row for
// the picker title.
func (m Model) mergeLineBudget() int {
	if m.height <= 0 {
		return 1 << 30
	}
	budget := m.listLineBudget() - 1 // room for the title line
	if budget < 1 {
		return 1
	}
	return budget
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
