package wstui

import (
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func seedItems() []models.Workstream {
	return []models.Workstream{
		{ID: "ws-a", Name: "Alpha", Workspace: "personal", State: models.StateActive, Focus: "alpha"},
		{ID: "ws-b", Name: "Beta", Workspace: "personal", State: models.StateActive, Focus: "beta"},
		models.NewDefaultWorkstream("personal", time.Time{}),
	}
}

func newSeededModel() Model {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	return m
}

// asModel unwraps tea.Model back to our concrete type for state assertions.
func asModel(t *testing.T, m tea.Model) Model {
	t.Helper()
	mm, ok := m.(Model)
	if !ok {
		t.Fatalf("expected wstui.Model, got %T", m)
	}
	return mm
}

func TestHandleListKey_NavigatesWithJK(t *testing.T) {
	m := newSeededModel()
	if m.cursor != 0 {
		t.Fatalf("cursor not zero at start: %d", m.cursor)
	}

	got, _ := m.Update(keyRune('j'))
	m = asModel(t, got)
	if m.cursor != 1 {
		t.Errorf("after j, cursor = %d, want 1", m.cursor)
	}

	got, _ = m.Update(keyRune('k'))
	m = asModel(t, got)
	if m.cursor != 0 {
		t.Errorf("after k, cursor = %d, want 0", m.cursor)
	}
}

func TestHandleListKey_RenameEntersEditMode(t *testing.T) {
	m := newSeededModel()
	got, _ := m.Update(keyRune('r'))
	m = asModel(t, got)
	if m.mode != modeEditName {
		t.Errorf("mode = %v, want modeEditName", m.mode)
	}
	if m.input != "Alpha" {
		t.Errorf("input = %q, want preloaded Alpha", m.input)
	}
}

func TestHandleListKey_RenameOnDefaultIsNoop(t *testing.T) {
	m := newSeededModel()
	m.cursor = len(m.items) - 1 // default is last after sort
	got, _ := m.Update(keyRune('r'))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("rename on default changed mode to %v", m.mode)
	}
}

func TestHandleListKey_DeleteOnDefaultIsNoop(t *testing.T) {
	m := newSeededModel()
	m.cursor = len(m.items) - 1
	got, _ := m.Update(keyRune('d'))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("delete on default changed mode to %v", m.mode)
	}
}

func TestHandleListKey_QuitsOnQ(t *testing.T) {
	m := newSeededModel()
	_, cmd := m.Update(keyRune('q'))
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", cmd())
	}
}

// TestCtrlC_QuitsFromAnyMode locks in the fix for the bug where Ctrl+C
// was only honored in modeList. Sub-modes (input, merge picker, delete
// confirm) handled `q`/`esc` as "exit sub-mode back to list" and ignored
// Ctrl+C, which meant a user who pressed n or m had no way to exit the
// program without killing the terminal.
func TestCtrlC_QuitsFromAnyMode(t *testing.T) {
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	for _, mode := range []mode{modeList, modeEditName, modeEditFocus, modeNewName, modeNewFocus, modeMergePick, modeConfirmDelete} {
		m := newSeededModel()
		m.mode = mode
		_, cmd := m.Update(ctrlC)
		if cmd == nil {
			t.Errorf("mode %v: expected quit cmd, got nil", mode)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("mode %v: expected QuitMsg, got %T", mode, cmd())
		}
	}
}

func TestHandleInputKey_TypeAndCommit(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeEditName
	m.input = "Alpha"

	// Backspace removes the last char.
	got, _ := m.Update(keyType(tea.KeyBackspace))
	m = asModel(t, got)
	if m.input != "Alph" {
		t.Errorf("after backspace, input = %q", m.input)
	}

	// Type two characters.
	got, _ = m.Update(keyRune('!'))
	m = asModel(t, got)
	got, _ = m.Update(keyRune('!'))
	m = asModel(t, got)
	if m.input != "Alph!!" {
		t.Errorf("after typing, input = %q", m.input)
	}

	// Enter commits via putCmd.
	got, cmd := m.Update(keyType(tea.KeyEnter))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("after enter, mode = %v, want modeList", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected put cmd")
	}
	// Drain the batch to fire the put.
	drainBatch(cmd)

	if len(st.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(st.puts))
	}
	if st.puts[0].Name != "Alph!!" {
		t.Errorf("persisted name = %q, want Alph!!", st.puts[0].Name)
	}
	if st.puts[0].ID != "ws-a" {
		t.Errorf("persisted ID changed: %q", st.puts[0].ID)
	}
}

func TestHandleInputKey_EscCancels(t *testing.T) {
	m := newSeededModel()
	m.mode = modeEditFocus
	m.input = "draft"
	got, _ := m.Update(keyType(tea.KeyEscape))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if m.input != "" {
		t.Errorf("input not cleared: %q", m.input)
	}
}

func TestCommitNewName_TransitionsToFocus(t *testing.T) {
	m := newSeededModel()
	m.mode = modeNewName
	m.input = "New Stream"
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = asModel(t, got)
	if m.mode != modeNewFocus {
		t.Errorf("mode = %v, want modeNewFocus", m.mode)
	}
	if m.scratchName != "New Stream" {
		t.Errorf("scratchName = %q", m.scratchName)
	}
	if m.input != "" {
		t.Errorf("input not cleared: %q", m.input)
	}
}

func TestCommitNewName_EmptyAborts(t *testing.T) {
	m := newSeededModel()
	m.mode = modeNewName
	m.input = "   "
	got, _ := m.Update(keyType(tea.KeyEnter))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
}

func TestCommitNewFocus_PersistsNewWorkstream(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeNewFocus
	m.scratchName = "Recommendations"
	m.input = "ranking"

	got, cmd := m.Update(keyType(tea.KeyEnter))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if m.scratchName != "" {
		t.Errorf("scratchName not cleared: %q", m.scratchName)
	}
	drainBatch(cmd)

	if len(st.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(st.puts))
	}
	w := st.puts[0]
	if w.Name != "Recommendations" {
		t.Errorf("name = %q", w.Name)
	}
	if w.ID != "ws-recommendations" {
		t.Errorf("id = %q", w.ID)
	}
	if w.Workspace != "personal" {
		t.Errorf("workspace = %q", w.Workspace)
	}
	if w.State != models.StateActive {
		t.Errorf("state = %q", w.State)
	}
	if w.Focus != "ranking" {
		t.Errorf("focus = %q", w.Focus)
	}
}

func TestConfirmDelete_YesPersistsDelete(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeConfirmDelete

	_, cmd := m.Update(keyRune('y'))
	drainBatch(cmd)

	if len(st.deletes) != 1 || st.deletes[0] != "ws-a" {
		t.Errorf("deletes = %v, want [ws-a]", st.deletes)
	}
}

func TestConfirmDelete_NoCancels(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeConfirmDelete

	got, _ := m.Update(keyRune('n'))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if len(st.deletes) != 0 {
		t.Errorf("delete fired on cancel: %v", st.deletes)
	}
}

func TestMergePicker_EnterMerges(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeMergePick
	m.cursor = 0      // Alpha is source
	m.mergeCursor = 1 // Beta is target

	_, cmd := m.Update(keyType(tea.KeyEnter))
	drainBatch(cmd)

	if len(st.puts) != 2 {
		t.Fatalf("expected 2 puts (target + retired source), got %d", len(st.puts))
	}
	// Target (Beta) put first per actions.go ordering.
	target := st.puts[0]
	if target.ID != "ws-b" {
		t.Errorf("first put should be target ws-b, got %q", target.ID)
	}
	if !contains(target.Focus, "merged from Alpha") {
		t.Errorf("target focus missing merge annotation: %q", target.Focus)
	}
	src := st.puts[1]
	if src.ID != "ws-a" {
		t.Errorf("second put should be source ws-a, got %q", src.ID)
	}
	if src.State != models.StateResolved {
		t.Errorf("source state = %q, want resolved", src.State)
	}
}

func TestMergePicker_EscCancels(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeMergePick

	got, _ := m.Update(keyType(tea.KeyEscape))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if len(st.puts) != 0 {
		t.Errorf("merge fired on cancel: %v", st.puts)
	}
}

func TestApplyLoaded_ClampsCursor(t *testing.T) {
	m := newSeededModel()
	m.cursor = 99
	out := m.applyLoaded(loadedMsg{items: []models.Workstream{m.items[0]}})
	if out.cursor != 0 {
		t.Errorf("cursor = %d, want 0", out.cursor)
	}
}

func TestApplyLoaded_SurfacesError(t *testing.T) {
	m := newSeededModel()
	out := m.applyLoaded(loadedMsg{err: errOnPut})
	if out.err == nil {
		t.Fatal("expected error to be set")
	}
}

func TestPutCmd_StoreErrorSurfacesAsLoadedMsgErr(t *testing.T) {
	st := newFakeStore()
	st.putErr = errOnPut
	m := NewModel(st, testCfg("personal"), nil)

	cmd := putCmd(m, models.Workstream{ID: "x", Name: "X", Workspace: "personal"}, "saved")
	gotErr := false
	for _, msg := range fanOut(cmd) {
		if lm, ok := msg.(loadedMsg); ok && lm.err != nil {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected loadedMsg with err from put failure")
	}
}

func TestFirstMergeTarget(t *testing.T) {
	if firstMergeTarget(0, 3) != 1 {
		t.Errorf("cursor 0, n 3: want 1, got %d", firstMergeTarget(0, 3))
	}
	if firstMergeTarget(2, 3) != 0 {
		t.Errorf("cursor 2, n 3: want 0, got %d", firstMergeTarget(2, 3))
	}
}

// drainBatch executes a tea.BatchMsg-producing cmd and any sub-cmds it
// returns synchronously, so store side-effects land before assertions.
func drainBatch(cmd tea.Cmd) {
	for _, msg := range fanOut(cmd) {
		_ = msg
	}
}

// fanOut returns every message produced by cmd, recursively expanding
// tea.BatchMsg and tea.Sequence into their component cmds. Sequence is
// detected by reflection because its sequenceMsg type is unexported.
//
// For sequence/batch, every component is fanned out (so callers see
// every msg in order — status flashes, store ops, and the eventual
// reload all surface). Timers (tea.Tick results) are returned as-is;
// tests that don't care just ignore them.
func fanOut(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, sub := range m {
			out = append(out, fanOut(sub)...)
		}
		return out
	case nil:
		return nil
	}
	// tea.Sequence wraps cmds in an unexported sequenceMsg, which is
	// `type sequenceMsg []tea.Cmd`. Use reflection to detect it and
	// drill into the FIRST component only — later components are
	// typically tea.Tick-based cleanups (e.g. the auto-clear after a
	// status flash) and invoking them would block tests on the timer.
	if subs, ok := unwrapSequence(msg); ok && len(subs) > 0 {
		return fanOut(subs[0])
	}
	return []tea.Msg{msg}
}

// unwrapSequence reflectively peeks at msg to extract the underlying
// []tea.Cmd if it is the unexported tea.sequenceMsg slice type.
func unwrapSequence(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]tea.Cmd, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i).Interface()
		c, ok := elem.(tea.Cmd)
		if !ok {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

// contains is a tiny strings.Contains shim so the test file doesn't
// need a strings import for one usage.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
