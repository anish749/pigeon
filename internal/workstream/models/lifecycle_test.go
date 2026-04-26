package models

import (
	"strings"
	"testing"
	"time"
)

func TestNewWorkstream(t *testing.T) {
	ts := time.Date(2026, 4, 26, 0, 0, 0, 0, time.UTC)
	w := NewWorkstream("Recommendations Engine", "personal", "ranking work", ts)

	if w.ID != "ws-recommendations-engine" {
		t.Errorf("ID = %q, want ws-recommendations-engine", w.ID)
	}
	if w.Name != "Recommendations Engine" {
		t.Errorf("Name = %q", w.Name)
	}
	if w.Workspace != "personal" {
		t.Errorf("Workspace = %q", w.Workspace)
	}
	if w.State != StateActive {
		t.Errorf("State = %q, want active", w.State)
	}
	if w.Focus != "ranking work" {
		t.Errorf("Focus = %q", w.Focus)
	}
	if !w.Created.Equal(ts) {
		t.Errorf("Created = %v, want %v", w.Created, ts)
	}
}

func TestNewWorkstream_NameWithSpacesAndPunctuation(t *testing.T) {
	w := NewWorkstream("Apple — Interview Process!", "x", "", time.Time{})
	if w.ID != "ws-apple-interview-process" {
		t.Errorf("ID = %q", w.ID)
	}
}

func TestNextState(t *testing.T) {
	cases := []struct {
		from, to WorkstreamState
	}{
		{StateActive, StateDormant},
		{StateDormant, StateResolved},
		{StateResolved, StateActive},
		{WorkstreamState("garbage"), StateActive},
	}
	for _, c := range cases {
		got := c.from.NextState()
		if got != c.to {
			t.Errorf("%q.NextState() = %q, want %q", c.from, got, c.to)
		}
	}
}

func TestMergeInto(t *testing.T) {
	src := Workstream{ID: "ws-a", Name: "A", State: StateActive, Focus: "ranking signal mix"}
	dst := Workstream{ID: "ws-b", Name: "B", State: StateActive, Focus: "deal review process"}

	gotDst, gotSrc := src.MergeInto(dst)

	if gotSrc.State != StateResolved {
		t.Errorf("source state = %q, want resolved", gotSrc.State)
	}
	if gotSrc.ID != "ws-a" || gotSrc.Focus != "ranking signal mix" {
		t.Errorf("source other fields mutated: %+v", gotSrc)
	}
	if !strings.Contains(gotDst.Focus, "deal review process") {
		t.Errorf("target focus lost original: %q", gotDst.Focus)
	}
	if !strings.Contains(gotDst.Focus, "[merged from A] ranking signal mix") {
		t.Errorf("target focus missing merge annotation: %q", gotDst.Focus)
	}
	if gotDst.ID != "ws-b" {
		t.Errorf("target ID changed: %q", gotDst.ID)
	}
}

func TestMergeInto_EmptySourceFocus(t *testing.T) {
	src := Workstream{ID: "ws-a", Name: "A", Focus: "   "}
	dst := Workstream{ID: "ws-b", Name: "B", Focus: "deal review"}

	gotDst, _ := src.MergeInto(dst)
	if gotDst.Focus != "deal review" {
		t.Errorf("empty source should not annotate; got %q", gotDst.Focus)
	}
}

func TestMergeInto_DuplicateFocusNotReAppended(t *testing.T) {
	src := Workstream{ID: "ws-a", Name: "A", Focus: "ranking work"}
	dst := Workstream{ID: "ws-b", Name: "B", Focus: "deal review\n\n[merged from A] ranking work"}

	gotDst, _ := src.MergeInto(dst)
	if strings.Count(gotDst.Focus, "ranking work") != 1 {
		t.Errorf("expected idempotent merge, got %q", gotDst.Focus)
	}
}
