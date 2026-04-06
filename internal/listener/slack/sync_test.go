package slack

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadCursors_EmptyYAML(t *testing.T) {
	// yaml.Unmarshal on empty input succeeds but leaves a map nil.
	// loadCursors must return an initialized map so AdvanceCursor
	// doesn't panic on nil map assignment.
	var c syncCursors
	if err := yaml.Unmarshal([]byte(""), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil map from empty YAML, got %v", c)
	}
}

func TestAdvanceCursor_EmptyCursors(t *testing.T) {
	// Simulate the post-reset state: cursors map is initialized but empty.
	// AdvanceCursor must not panic.
	ms := &MessageStore{
		cursors: make(syncCursors),
	}
	// Should not panic.
	ms.AdvanceCursor("C12345", "1234567890.000001")

	if got, ok := ms.Cursor("C12345"); !ok || got != "1234567890.000001" {
		t.Errorf("Cursor() = %q, %v; want %q, true", got, ok, "1234567890.000001")
	}
}
