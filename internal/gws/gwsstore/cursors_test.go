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
			"primary":       "sync-1",
			"work@test.com": "sync-2",
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
	if got.Calendar["primary"] != "sync-1" {
		t.Errorf("Calendar[primary] = %q, want %q", got.Calendar["primary"], "sync-1")
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
