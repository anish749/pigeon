package slack

import "testing"

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
