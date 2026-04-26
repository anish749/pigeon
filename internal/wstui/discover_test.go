package wstui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/workstream/models"
)

// stubDiscover returns a DiscoverFunc that records call count and
// returns the configured (count, err). Tests use it to drive the
// discovery flow without a real LLM.
func stubDiscover(count int, err error) (DiscoverFunc, *atomic.Int32) {
	calls := &atomic.Int32{}
	fn := func(ctx context.Context) (int, error) {
		calls.Add(1)
		return count, err
	}
	return fn, calls
}

func TestPressD_StartsDiscovery(t *testing.T) {
	fn, calls := stubDiscover(3, nil)
	st := newFakeStore(seedItems()...)
	m := NewModel(st, "personal", fn)
	m.items = filterAndSort(seedItems(), "personal")

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeDiscovering {
		t.Errorf("mode = %v, want modeDiscovering", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected discover cmd")
	}

	// Discovery cmd is a tea.Batch of (spinTick, async fn). Drain it
	// and feed the resulting messages back into Update so the model
	// applies them.
	for _, msg := range fanOut(cmd) {
		got, _ = m.Update(msg)
		m = asModel(t, got)
	}
	if calls.Load() != 1 {
		t.Errorf("DiscoverFunc called %d times, want 1", calls.Load())
	}
	if m.mode != modeList {
		t.Errorf("after completion, mode = %v, want modeList", m.mode)
	}
}

func TestPressD_NoopWhenDiscoverFnNil(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, "personal", nil)
	m.items = filterAndSort(seedItems(), "personal")

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("D with nil fn changed mode to %v", m.mode)
	}
	if cmd != nil {
		t.Errorf("expected no cmd, got %T", cmd())
	}
}

func TestPressD_FromEmptyWorkspace(t *testing.T) {
	fn, calls := stubDiscover(2, nil)
	st := newFakeStore() // no seeds — empty workspace
	m := NewModel(st, "personal", fn)

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeDiscovering {
		t.Fatalf("mode = %v, want modeDiscovering", m.mode)
	}
	for _, msg := range fanOut(cmd) {
		got, _ = m.Update(msg)
		m = asModel(t, got)
	}
	if calls.Load() != 1 {
		t.Errorf("DiscoverFunc not called from empty state")
	}
}

func TestSpinTickAdvancesFrameOnlyWhileDiscovering(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, "personal", nil)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeDiscovering

	got, cmd := m.Update(spinTickMsg{})
	m = asModel(t, got)
	if m.spinnerFrame != 1 {
		t.Errorf("after tick, frame = %d, want 1", m.spinnerFrame)
	}
	if cmd == nil {
		t.Error("expected next-tick cmd")
	}

	// Once mode flips back to list, ticks should be ignored.
	m.mode = modeList
	got, cmd = m.Update(spinTickMsg{})
	m = asModel(t, got)
	if m.spinnerFrame != 1 {
		t.Errorf("frame advanced outside modeDiscovering: %d", m.spinnerFrame)
	}
	if cmd != nil {
		t.Error("expected no further tick cmd")
	}
}

func TestApplyDiscoverDone_SuccessFlashesAndReloads(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, "personal", nil)
	m.mode = modeDiscovering
	m.spinnerFrame = 7

	got, cmd := m.applyDiscoverDone(discoverDoneMsg{count: 5})
	m = asModel(t, got)

	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if m.spinnerFrame != 0 {
		t.Errorf("spinnerFrame = %d, want 0", m.spinnerFrame)
	}
	if cmd == nil {
		t.Fatal("expected status+reload cmd")
	}

	sawStatus := false
	for _, msg := range fanOut(cmd) {
		if s, ok := msg.(statusMsg); ok && strings.Contains(string(s), "discovered 5 workstreams") {
			sawStatus = true
		}
	}
	if !sawStatus {
		t.Error("expected status flash with discovered count")
	}
}

func TestApplyDiscoverDone_ZeroCountSaysNoneFound(t *testing.T) {
	st := newFakeStore()
	m := NewModel(st, "personal", nil)
	m.mode = modeDiscovering

	_, cmd := m.applyDiscoverDone(discoverDoneMsg{count: 0})
	for _, msg := range fanOut(cmd) {
		if s, ok := msg.(statusMsg); ok {
			if !strings.Contains(string(s), "no workstreams") {
				t.Errorf("status = %q, want contains 'no workstreams'", string(s))
			}
		}
	}
}

func TestApplyDiscoverDone_ErrorSurfacedNoStatus(t *testing.T) {
	st := newFakeStore()
	m := NewModel(st, "personal", nil)
	m.mode = modeDiscovering

	got, cmd := m.applyDiscoverDone(discoverDoneMsg{err: errors.New("LLM exploded")})
	m = asModel(t, got)

	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList", m.mode)
	}
	if m.err == nil {
		t.Fatal("expected err to be set")
	}
	if cmd != nil {
		t.Error("expected no follow-up cmd on error path")
	}
}

func TestKeyDuringDiscovery_NotForwardedExceptCtrlC(t *testing.T) {
	fn, calls := stubDiscover(1, nil)
	st := newFakeStore(seedItems()...)
	m := NewModel(st, "personal", fn)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeDiscovering

	// Pressing D again must not re-fire discovery.
	got, _ := m.Update(keyRune('D'))
	m = asModel(t, got)
	if calls.Load() != 0 {
		t.Errorf("D during discovery re-fired DiscoverFunc")
	}
	if m.mode != modeDiscovering {
		t.Errorf("mode changed during discovery: %v", m.mode)
	}

	// Ctrl+C must still quit (locked in by Update_test's
	// TestCtrlC_QuitsFromAnyMode, but verify it survives the new mode).
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c during discovery should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c should produce QuitMsg, got %T", cmd())
	}
}

// TestModeDiscovering_InCtrlCSurvey extends the all-modes Ctrl+C check
// in update_test.go to cover the new mode without modifying that test.
func TestModeDiscovering_InCtrlCSurvey(t *testing.T) {
	st := newFakeStore()
	m := NewModel(st, "personal", nil)
	m.mode = modeDiscovering

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", cmd())
	}
}

// silence unused-import lint when models import is only used elsewhere.
var _ = models.StateActive
