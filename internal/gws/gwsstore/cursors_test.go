package gwsstore

import (
	"path/filepath"
	"testing"
)

func TestCursorsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursors.yaml")

	orig := &Cursors{
		Gmail: GmailCursors{HistoryID: "12345"},
		Drive: DriveCursors{PageToken: "tok-abc"},
		Calendar: CalendarCursors{
			"primary": {
				SyncToken:       "sync-1",
				ExpandedUntil:   "2026-07-07T00:00:00Z",
				RecurringEvents: []string{"evt-a", "evt-b"},
			},
			"work@test.com": {
				SyncToken: "sync-2",
			},
		},
	}

	if err := SaveCursors(path, orig); err != nil {
		t.Fatalf("SaveCursors: %v", err)
	}

	got, err := LoadCursors(path)
	if err != nil {
		t.Fatalf("LoadCursors: %v", err)
	}

	if got.Gmail.HistoryID != orig.Gmail.HistoryID {
		t.Errorf("Gmail.HistoryID = %q, want %q", got.Gmail.HistoryID, orig.Gmail.HistoryID)
	}
	if got.Drive.PageToken != orig.Drive.PageToken {
		t.Errorf("Drive.PageToken = %q, want %q", got.Drive.PageToken, orig.Drive.PageToken)
	}
	if len(got.Calendar) != 2 {
		t.Fatalf("Calendar has %d entries, want 2", len(got.Calendar))
	}
	primary := got.Calendar["primary"]
	if primary == nil {
		t.Fatal("Calendar[primary] is nil")
	}
	if primary.SyncToken != "sync-1" {
		t.Errorf("Calendar[primary].SyncToken = %q, want %q", primary.SyncToken, "sync-1")
	}
	if primary.ExpandedUntil != "2026-07-07T00:00:00Z" {
		t.Errorf("Calendar[primary].ExpandedUntil = %q, want %q", primary.ExpandedUntil, "2026-07-07T00:00:00Z")
	}
	if len(primary.RecurringEvents) != 2 {
		t.Fatalf("Calendar[primary].RecurringEvents has %d entries, want 2", len(primary.RecurringEvents))
	}
}

func TestLoadCursorsNonExistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	got, err := LoadCursors(path)
	if err != nil {
		t.Fatalf("LoadCursors: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil Cursors")
	}
	if got.Gmail.HistoryID != "" {
		t.Errorf("Gmail.HistoryID = %q, want empty", got.Gmail.HistoryID)
	}
}
