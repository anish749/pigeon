package workstream

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

func TestFilterAndSort_ScopesToWorkspace(t *testing.T) {
	all := []models.Workstream{
		{ID: "ws-a", Name: "A", Workspace: "personal"},
		{ID: "ws-b", Name: "B", Workspace: "tubular"},
		{ID: "ws-c", Name: "C", Workspace: "personal"},
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
		{ID: "ws-aaa", Name: "AAA", Workspace: "personal"},
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

func TestFilterAndSort_AlphabeticalByName(t *testing.T) {
	all := []models.Workstream{
		{ID: "z", Name: "Zeta", Workspace: "x"},
		{ID: "a", Name: "alpha", Workspace: "x"},
		{ID: "b", Name: "Beta", Workspace: "x"},
		{ID: "y", Name: "Yankee", Workspace: "x"},
	}
	got := filterAndSort(all, "x")
	wantOrder := []string{"alpha", "Beta", "Yankee", "Zeta"}
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
