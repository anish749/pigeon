package wstui

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func TestFilterAndSort_ScopesToWorkspace(t *testing.T) {
	all := []models.Workstream{
		{ID: "ws-a", Name: "A", Workspace: "personal", State: models.StateActive},
		{ID: "ws-b", Name: "B", Workspace: "tubular", State: models.StateActive},
		{ID: "ws-c", Name: "C", Workspace: "personal", State: models.StateActive},
	}
	got := filterAndSort(all, "personal")
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	for _, w := range got {
		if w.Workspace != "personal" {
			t.Errorf("leaked workspace %q", w.Workspace)
		}
	}
}

func TestFilterAndSort_DefaultLast(t *testing.T) {
	def := models.NewDefaultWorkstream("personal", time.Time{})
	all := []models.Workstream{
		def,
		{ID: "ws-aaa", Name: "AAA", Workspace: "personal", State: models.StateActive},
	}
	got := filterAndSort(all, "personal")
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	if got[0].IsDefault() {
		t.Errorf("default sorted first; want last")
	}
	if !got[1].IsDefault() {
		t.Errorf("default not last")
	}
}

func TestFilterAndSort_StateThenName(t *testing.T) {
	all := []models.Workstream{
		{ID: "z", Name: "Zeta", Workspace: "x", State: models.StateActive},
		{ID: "a", Name: "Alpha", Workspace: "x", State: models.StateResolved},
		{ID: "b", Name: "Beta", Workspace: "x", State: models.StateDormant},
		{ID: "y", Name: "Yankee", Workspace: "x", State: models.StateActive},
	}
	got := filterAndSort(all, "x")
	wantOrder := []string{"Yankee", "Zeta", "Beta", "Alpha"}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("position %d: got %q, want %q", i, got[i].Name, name)
		}
	}
}

func TestFilterAndSort_Empty(t *testing.T) {
	if got := filterAndSort(nil, "x"); len(got) != 0 {
		t.Errorf("got %d items from nil input", len(got))
	}
}

func TestStateRank(t *testing.T) {
	if stateRank(models.StateActive) >= stateRank(models.StateDormant) {
		t.Error("active should rank before dormant")
	}
	if stateRank(models.StateDormant) >= stateRank(models.StateResolved) {
		t.Error("dormant should rank before resolved")
	}
	if stateRank(models.WorkstreamState("garbage")) <= stateRank(models.StateResolved) {
		t.Error("unknown state should rank last")
	}
}
