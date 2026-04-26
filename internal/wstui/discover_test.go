package wstui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// fakeManager satisfies the wstui.Manager interface with a configurable
// canned response. It records call count and the (since, until) it was
// invoked with so tests can assert the model passes the cfg window
// through unchanged.
type fakeManager struct {
	calls    atomic.Int32
	gotSince time.Time
	gotUntil time.Time

	count int
	err   error
}

func (f *fakeManager) DiscoverAndPropose(_ context.Context, since, until, _ time.Time) ([]discovery.DiscoveredWorkstream, error) {
	f.calls.Add(1)
	f.gotSince = since
	f.gotUntil = until
	if f.err != nil {
		return nil, f.err
	}
	out := make([]discovery.DiscoveredWorkstream, f.count)
	return out, nil
}

func TestPressD_StartsDiscoveryWithConfigWindow(t *testing.T) {
	mgr := &fakeManager{count: 3}
	cfg := testCfg("personal")
	cfg.Since = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	cfg.Until = time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)
	st := newFakeStore(seedItems()...)
	m := NewModel(st, cfg, mgr)
	m.items = filterAndSort(seedItems(), "personal")

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeDiscovering {
		t.Errorf("mode = %v, want modeDiscovering", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected discover cmd")
	}

	for _, msg := range fanOut(cmd) {
		got, _ = m.Update(msg)
		m = asModel(t, got)
	}
	if mgr.calls.Load() != 1 {
		t.Errorf("DiscoverAndPropose called %d times, want 1", mgr.calls.Load())
	}
	if !mgr.gotSince.Equal(cfg.Since) {
		t.Errorf("since = %v, want cfg.Since %v", mgr.gotSince, cfg.Since)
	}
	if !mgr.gotUntil.Equal(cfg.Until) {
		t.Errorf("until = %v, want cfg.Until %v", mgr.gotUntil, cfg.Until)
	}
	if m.mode != modeList {
		t.Errorf("after completion, mode = %v, want modeList", m.mode)
	}
}

func TestPressD_NoopWhenManagerNil(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
	m.items = filterAndSort(seedItems(), "personal")

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeList {
		t.Errorf("D with nil manager changed mode to %v", m.mode)
	}
	if cmd != nil {
		t.Errorf("expected no cmd, got %T", cmd())
	}
}

func TestPressD_FromEmptyWorkspace(t *testing.T) {
	mgr := &fakeManager{count: 2}
	st := newFakeStore() // no seeds — empty workspace
	m := NewModel(st, testCfg("personal"), mgr)

	got, cmd := m.Update(keyRune('D'))
	m = asModel(t, got)
	if m.mode != modeDiscovering {
		t.Fatalf("mode = %v, want modeDiscovering", m.mode)
	}
	for _, msg := range fanOut(cmd) {
		got, _ = m.Update(msg)
		m = asModel(t, got)
	}
	if mgr.calls.Load() != 1 {
		t.Errorf("DiscoverAndPropose not called from empty state")
	}
}

func TestSpinTickAdvancesFrameOnlyWhileDiscovering(t *testing.T) {
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), nil)
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
	m := NewModel(st, testCfg("personal"), nil)
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
	m := NewModel(st, testCfg("personal"), nil)
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
	m := NewModel(st, testCfg("personal"), nil)
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
	mgr := &fakeManager{count: 1}
	st := newFakeStore(seedItems()...)
	m := NewModel(st, testCfg("personal"), mgr)
	m.items = filterAndSort(seedItems(), "personal")
	m.mode = modeDiscovering

	// Pressing D again must not re-fire discovery.
	got, _ := m.Update(keyRune('D'))
	m = asModel(t, got)
	if mgr.calls.Load() != 0 {
		t.Errorf("D during discovery re-fired DiscoverAndPropose")
	}
	if m.mode != modeDiscovering {
		t.Errorf("mode changed during discovery: %v", m.mode)
	}

	// Ctrl+C must still quit.
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
	m := NewModel(st, testCfg("personal"), nil)
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
