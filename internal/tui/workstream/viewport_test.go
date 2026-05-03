package workstream

import (
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func TestViewport_AllItemsFit(t *testing.T) {
	heights := []int{1, 1, 1, 1}
	start, end := viewport(4, 1, 0, 100, func(i int) int { return heights[i] })
	if start != 0 || end != 4 {
		t.Errorf("got [%d,%d), want [0,4)", start, end)
	}
}

func TestViewport_NoBudget(t *testing.T) {
	start, end := viewport(10, 5, 0, 0, func(int) int { return 1 })
	if start != 0 || end != 10 {
		t.Errorf("zero budget should return whole range, got [%d,%d)", start, end)
	}
}

func TestViewport_PushesOffsetForwardWhenCursorOffBottom(t *testing.T) {
	start, end := viewport(20, 15, 0, 5, func(int) int { return 1 })
	if start > 15 || end <= 15 {
		t.Errorf("cursor not in [%d,%d)", start, end)
	}
}

func TestViewport_PullsOffsetBackWhenCursorAbove(t *testing.T) {
	start, _ := viewport(20, 3, 10, 5, func(int) int { return 1 })
	if start > 3 {
		t.Errorf("offset should pull back to cursor, got %d", start)
	}
}

func TestViewport_VariableHeights(t *testing.T) {
	heights := []int{1, 2, 1, 3, 1, 1}
	start, end := viewport(len(heights), 0, 0, 5, func(i int) int { return heights[i] })
	if start != 0 {
		t.Errorf("start = %d, want 0", start)
	}
	used := 0
	for i := start; i < end; i++ {
		used += heights[i]
	}
	if used > 5 {
		t.Errorf("rendered %d lines, exceeds budget 5", used)
	}
}

func TestRenderMergePicker_ScrollsLongCandidateList(t *testing.T) {
	items := []models.Workstream{
		models.NewDefaultWorkstream("acme", time.Time{}),
		{ID: "src", Name: "Source", Workspace: "acme"},
	}
	for i := 0; i < 30; i++ {
		items = append(items, models.Workstream{ID: "c", Name: "Candidate", Workspace: "acme"})
	}
	m := NewModel(newFakeStore(), testCfg("acme"), nil)
	m.items = items
	m.cursor = 1
	m.height = 12
	m.width = 80

	upd, _ := m.Update(keyRune('m'))
	m = upd.(Model)
	out := stripAnsi(m.View())
	rendered := strings.Count(out, "Candidate")
	if rendered >= 30 {
		t.Errorf("expected truncated render, got %d candidate rows for 30 items", rendered)
	}
	if rendered < 1 {
		t.Errorf("expected at least one candidate row")
	}
}

func TestRenderMergePicker_KeepsCursorInView(t *testing.T) {
	items := []models.Workstream{
		models.NewDefaultWorkstream("acme", time.Time{}),
		{ID: "src", Name: "Source", Workspace: "acme"},
	}
	for i := 'A'; i <= 'Z'; i++ {
		items = append(items, models.Workstream{ID: "c", Name: "Item " + string(i), Workspace: "acme"})
	}
	m := NewModel(newFakeStore(), testCfg("acme"), nil)
	m.items = items
	m.cursor = 1
	m.height = 10
	m.width = 80

	upd, _ := m.Update(keyRune('m'))
	m = upd.(Model)
	for i := 0; i < 20; i++ {
		upd, _ = m.Update(keyRune('j'))
		m = upd.(Model)
	}
	if m.mergeCursor < 0 || m.mergeCursor >= len(m.items) {
		t.Fatalf("invalid mergeCursor %d", m.mergeCursor)
	}
	cursorName := m.items[m.mergeCursor].Name
	out := stripAnsi(m.View())
	if !strings.Contains(out, cursorName) {
		t.Errorf("cursor %q not visible after scroll:\n%s", cursorName, out)
	}
	if !strings.Contains(out, "→ "+cursorName) {
		t.Errorf("cursor marker missing for %q:\n%s", cursorName, out)
	}
}

func TestRenderMergePicker_ScrollsBackWhenCursorMovesUp(t *testing.T) {
	items := []models.Workstream{
		models.NewDefaultWorkstream("acme", time.Time{}),
		{ID: "src", Name: "Source", Workspace: "acme"},
	}
	for i := 'A'; i <= 'Z'; i++ {
		items = append(items, models.Workstream{ID: "c", Name: "Item " + string(i), Workspace: "acme"})
	}
	m := NewModel(newFakeStore(), testCfg("acme"), nil)
	m.items = items
	m.cursor = 1
	m.height = 10
	m.width = 80

	upd, _ := m.Update(keyRune('m'))
	m = upd.(Model)
	for i := 0; i < 20; i++ {
		upd, _ = m.Update(keyRune('j'))
		m = upd.(Model)
	}
	for i := 0; i < 20; i++ {
		upd, _ = m.Update(keyRune('k'))
		m = upd.(Model)
	}
	cursorName := m.items[m.mergeCursor].Name
	out := stripAnsi(m.View())
	if !strings.Contains(out, "→ "+cursorName) {
		t.Errorf("cursor marker missing after scrolling back up to %q:\n%s", cursorName, out)
	}
}

func TestEnterMerge_InitializesOffset(t *testing.T) {
	m := newSeededModel()
	upd, _ := m.Update(keyRune('m'))
	m = upd.(Model)
	if m.mode != modeMergePick {
		t.Fatalf("did not enter merge mode")
	}
	if m.mergeOffset != m.mergeCursor {
		t.Errorf("mergeOffset = %d, want %d to match initial cursor", m.mergeOffset, m.mergeCursor)
	}
}
