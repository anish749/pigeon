package wstui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func TestWrapName_FitsOnOneLine(t *testing.T) {
	got := wrapName("Short Name", 20, "  ")
	if len(got) != 1 || got[0] != "Short Name" {
		t.Fatalf("wrapName = %q, want single line %q", got, "Short Name")
	}
}

func TestWrapName_WrapsWithIndent(t *testing.T) {
	got := wrapName("Alpha Beta Gamma Delta Epsilon", 12, "  ")
	if len(got) < 2 {
		t.Fatalf("wrapName produced %d lines, want >=2: %q", len(got), got)
	}
	for i, line := range got[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("continuation line %d %q missing indent", i+1, line)
		}
	}
	for i, line := range got {
		if w := lipgloss.Width(line); w > 12 {
			t.Errorf("line %d %q width %d exceeds 12", i, line, w)
		}
	}
}

func TestWrapName_SingleWordLongerThanWidth(t *testing.T) {
	got := wrapName("Supercalifragilisticexpialidocious", 10, "  ")
	if len(got) != 1 {
		t.Fatalf("oversized single word should not be split, got %q", got)
	}
}

func TestWrapName_Empty(t *testing.T) {
	got := wrapName("", 10, "  ")
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("wrapName(\"\") = %q, want [\"\"]", got)
	}
}

func TestColumnWidths_DefaultWhenUnset(t *testing.T) {
	m := Model{}
	left, right := m.columnWidths()
	if left < minListWidth || left > maxListWidth {
		t.Errorf("left = %d, want in [%d,%d]", left, minListWidth, maxListWidth)
	}
	if right < minDetailWidth {
		t.Errorf("right = %d, want >= %d", right, minDetailWidth)
	}
}

func TestColumnWidths_RespectsTotal(t *testing.T) {
	m := Model{width: 120}
	left, right := m.columnWidths()
	want := 120 - 2*bodyMargin - bodyGap
	if left+right != want {
		t.Errorf("left(%d)+right(%d)=%d, want %d", left, right, left+right, want)
	}
}

func TestColumnWidths_LeftClampedAtMax(t *testing.T) {
	m := Model{width: 500}
	left, _ := m.columnWidths()
	if left != maxListWidth {
		t.Errorf("left = %d, want clamp to %d on wide terminal", left, maxListWidth)
	}
}

func TestRenderListColumn_CursorMarker(t *testing.T) {
	m := Model{
		items: []models.Workstream{
			{ID: "a", Name: "Alpha"},
			{ID: "b", Name: "Beta"},
		},
		cursor: 1,
	}
	out := stripAnsi(m.renderListColumn(20))
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "○ Alpha") {
		t.Errorf("non-cursor line lacks hollow marker: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "● Beta") {
		t.Errorf("cursor line lacks filled marker: %q", lines[1])
	}
}

func TestRenderListColumn_WrapsLongNameWithContinuationIndent(t *testing.T) {
	m := Model{
		items: []models.Workstream{
			{ID: "anchor", Name: "Anchor"},
			{ID: "long", Name: "Alpha Beta Gamma Delta Epsilon Zeta"},
		},
		cursor: 0,
	}
	out := stripAnsi(m.renderListColumn(18))
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected anchor + wrapped name spanning multiple lines, got %q", lines)
	}
	wrapped := lines[1:]
	if !strings.HasPrefix(wrapped[0], "○ Alpha") {
		t.Errorf("non-cursor first line lacks hollow marker: %q", wrapped[0])
	}
	for i, line := range wrapped[1:] {
		if !strings.HasPrefix(line, "    ") {
			t.Errorf("continuation %d %q should start with marker(2)+indent(2) = 4 spaces", i+1, line)
		}
	}
}

func TestRenderListColumn_DefaultLabelOnLastLine(t *testing.T) {
	m := Model{
		items: []models.Workstream{
			{ID: "_default_acme", Name: "General", Workspace: "acme"},
		},
	}
	out := stripAnsi(m.renderListColumn(40))
	if !strings.Contains(out, "General (default)") {
		t.Errorf("default suffix missing from %q", out)
	}
}

func TestRenderListColumn_DefaultLabelOnLastWrappedLine(t *testing.T) {
	m := Model{
		items: []models.Workstream{
			{
				ID:        "_default_acme",
				Name:      "General Catch All Workstream Name",
				Workspace: "acme",
			},
		},
	}
	out := stripAnsi(m.renderListColumn(16))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[len(lines)-1], "(default)") {
		t.Errorf("default suffix should land on last wrapped line, got lines=%q", lines)
	}
	for i, line := range lines[:len(lines)-1] {
		if strings.Contains(line, "(default)") {
			t.Errorf("default suffix appears on non-last line %d: %q", i, line)
		}
	}
}

func TestRenderDetailBox_ShowsNameAndFocus(t *testing.T) {
	m := Model{}
	w := models.Workstream{ID: "ws-a", Name: "Alpha", Workspace: "acme", Focus: "Some focus text"}
	out := stripAnsi(m.renderDetailBox(w, minDetailWidth))
	if !strings.Contains(out, "Alpha") {
		t.Errorf("box missing workstream name: %q", out)
	}
	if !strings.Contains(out, "Some focus text") {
		t.Errorf("box missing focus: %q", out)
	}
}

func TestRenderDetailBox_OmitsWorkspaceName(t *testing.T) {
	m := Model{}
	w := models.Workstream{ID: "ws-a", Name: "Alpha", Workspace: "acme", Focus: "Some focus"}
	out := stripAnsi(m.renderDetailBox(w, minDetailWidth))
	if strings.Contains(out, "acme") {
		t.Errorf("box should not include workspace name: %q", out)
	}
	if strings.Contains(out, "Workspace:") {
		t.Errorf("box should not have a Workspace label: %q", out)
	}
}

func TestRenderDetailBox_NoFocusFallback(t *testing.T) {
	m := Model{}
	w := models.Workstream{ID: "ws-a", Name: "Alpha", Workspace: "acme"}
	out := stripAnsi(m.renderDetailBox(w, minDetailWidth))
	if !strings.Contains(out, "(no focus set)") {
		t.Errorf("missing focus fallback: %q", out)
	}
}

func TestRenderDetailBox_DefaultLabel(t *testing.T) {
	m := Model{}
	w := models.Workstream{ID: "_default_acme", Name: "General", Workspace: "acme"}
	out := stripAnsi(m.renderDetailBox(w, minDetailWidth))
	if !strings.Contains(out, "General (default)") {
		t.Errorf("default suffix missing: %q", out)
	}
}

func TestRenderDetailBox_ReservesTrailingSpace(t *testing.T) {
	m := Model{}
	w := models.Workstream{ID: "ws-a", Name: "Alpha", Workspace: "acme", Focus: "Some focus"}
	out := stripAnsi(m.renderDetailBox(w, minDetailWidth))
	lines := strings.Split(out, "\n")
	// The bottom border is the last line; the line above it should be
	// blank content (interior of the box) reserving room for new fields.
	if len(lines) < 4 {
		t.Fatalf("box too short: %q", lines)
	}
	beforeBottom := lines[len(lines)-2]
	if strings.Contains(beforeBottom, "Focus") || strings.Contains(beforeBottom, "Alpha") {
		t.Errorf("expected blank reserved row above bottom border, got %q", beforeBottom)
	}
}

func TestVisibleRange_ScrollsCursorIntoView(t *testing.T) {
	items := make([]models.Workstream, 30)
	for i := range items {
		items[i] = models.Workstream{ID: "ws", Name: "Item"}
	}
	m := Model{
		items:      items,
		cursor:     20,
		listOffset: 0,
		height:     12, // tight budget
		width:      100,
	}
	m = m.scrollIntoView()
	start, end := m.visibleRange(20)
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("cursor %d not in visible range [%d,%d)", m.cursor, start, end)
	}
}

func TestVisibleRange_StablyAnchorsAtTop(t *testing.T) {
	items := make([]models.Workstream, 30)
	for i := range items {
		items[i] = models.Workstream{ID: "ws", Name: "Item"}
	}
	m := Model{
		items:      items,
		cursor:     2,
		listOffset: 0,
		height:     12,
		width:      100,
	}
	m = m.scrollIntoView()
	start, _ := m.visibleRange(20)
	if start != 0 {
		t.Errorf("cursor near top should keep start=0, got %d", start)
	}
}

func TestVisibleRange_ScrollsBackUpWhenCursorMovesUp(t *testing.T) {
	items := make([]models.Workstream, 30)
	for i := range items {
		items[i] = models.Workstream{ID: "ws", Name: "Item"}
	}
	m := Model{
		items:      items,
		cursor:     20,
		listOffset: 12,
		height:     12,
		width:      100,
	}
	m.cursor = 5
	m = m.scrollIntoView()
	start, end := m.visibleRange(20)
	if start > 5 || end <= 5 {
		t.Errorf("cursor at 5 not in [%d,%d)", start, end)
	}
}

func TestRenderListColumn_TruncatesToBudget(t *testing.T) {
	items := make([]models.Workstream, 30)
	for i := range items {
		items[i] = models.Workstream{ID: "ws", Name: "Item"}
	}
	m := Model{
		items:  items,
		cursor: 0,
		height: 10,
		width:  100,
	}
	out := stripAnsi(m.renderListColumn(20))
	rendered := strings.Count(out, "\n") + 1
	if rendered >= 30 {
		t.Errorf("expected truncated render, got %d lines for 30 items", rendered)
	}
	if rendered < 1 {
		t.Errorf("expected at least the cursor row, got %d lines", rendered)
	}
}

func TestView_SideBySideKeepsNameAndFocusVisible(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("acme"), nil)
	m.width = 100
	m.height = 20
	m.items = []models.Workstream{
		{ID: "ws-a", Name: "Alpha", Workspace: "acme", Focus: "alpha focus"},
		{ID: "ws-b", Name: "Beta", Workspace: "acme", Focus: "beta focus"},
	}
	out := stripAnsi(m.View())
	for _, want := range []string{"Alpha", "Beta", "alpha focus"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered view missing %q in:\n%s", want, out)
		}
	}
	// Verify the list and box are actually on the same line — find a
	// line that has the cursor row's name AND the box's left border.
	hasJoined := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Alpha") && strings.Contains(line, "╭") {
			hasJoined = true
			break
		}
	}
	if !hasJoined {
		t.Errorf("expected list and box on same line, got:\n%s", out)
	}
}
