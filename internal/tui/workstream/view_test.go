package workstream

import (
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

// stripAnsi removes ANSI escape sequences so substring assertions on
// the rendered View aren't fooled by lipgloss color codes.
func stripAnsi(s string) string {
	var b strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			skip = true
		case skip && (r == 'm' || r == 'K' || r == 'H'):
			skip = false
		case skip:
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestView_EmptyShowsCreateHint(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	out := stripAnsi(m.View())
	if !strings.Contains(out, "No workstreams in this workspace") {
		t.Errorf("missing empty header: %q", out)
	}
	if !strings.Contains(out, "n new") {
		t.Errorf("missing n hint: %q", out)
	}
}

func TestView_PopulatedRendersAllItems(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	m.items = []models.Workstream{
		{ID: "ws-a", Name: "Alpha", Workspace: "personal", Focus: "alpha focus"},
		{ID: "ws-b", Name: "Beta", Workspace: "personal", Focus: "beta focus"},
	}
	out := stripAnsi(m.View())
	for _, want := range []string{"Alpha", "Beta", "alpha focus"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered view missing %q in:\n%s", want, out)
		}
	}
}

func TestView_DefaultRowGetsLabel(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	def := models.NewDefaultWorkstream("personal", time.Time{})
	m.items = []models.Workstream{def}
	out := stripAnsi(m.View())
	if !strings.Contains(out, "(default)") {
		t.Errorf("missing default marker: %q", out)
	}
}

func TestView_DefaultHelpIsLimited(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	def := models.NewDefaultWorkstream("personal", time.Time{})
	m.items = []models.Workstream{def}
	out := stripAnsi(m.View())
	if !strings.Contains(out, "default workstream: limited actions") {
		t.Errorf("default help should call out limited actions: %q", out)
	}
	if strings.Contains(out, "merge") || strings.Contains(out, "delete") {
		t.Errorf("default help should not list merge/delete: %q", out)
	}
}

func TestView_DeleteConfirmShowsName(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	m.items = []models.Workstream{
		{ID: "ws-a", Name: "Alpha", Workspace: "personal"},
	}
	m.mode = modeConfirmDelete
	out := stripAnsi(m.View())
	if !strings.Contains(out, `Delete "Alpha"?`) {
		t.Errorf("missing confirm prompt: %q", out)
	}
}

func TestView_FillsTerminalHeightWithFooterAtBottom(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	m.height = 12
	m.items = []models.Workstream{
		{ID: "ws-a", Name: "Alpha", Workspace: "personal", Focus: "alpha focus"},
	}

	lines := strings.Split(stripAnsi(m.View()), "\n")
	if got := len(lines); got != m.height {
		t.Fatalf("rendered lines = %d, want %d:\n%s", got, m.height, strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") {
		t.Fatalf("footer not pinned to bottom, last line = %q", lines[len(lines)-1])
	}
}

func TestView_TrimsOverflowBeforeFooter(t *testing.T) {
	m := NewModel(newFakeStore(), testCfg("personal"), nil)
	m.height = 8
	for i := 0; i < 20; i++ {
		m.items = append(m.items, models.Workstream{
			ID:        "ws",
			Name:      "Workstream",
			Workspace: "personal",
		})
	}

	lines := strings.Split(stripAnsi(m.View()), "\n")
	if got := len(lines); got != m.height {
		t.Fatalf("rendered lines = %d, want %d:\n%s", got, m.height, strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") {
		t.Fatalf("footer should survive overflow, last line = %q", lines[len(lines)-1])
	}
}

func TestEmptyOr(t *testing.T) {
	if emptyOr("hello", "fallback") != "hello" {
		t.Error("non-empty input should be returned as-is")
	}
	if emptyOr("   ", "fallback") != "fallback" {
		t.Error("whitespace input should yield fallback")
	}
	if emptyOr("", "fallback") != "fallback" {
		t.Error("empty input should yield fallback")
	}
}
