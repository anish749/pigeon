package gwsstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

func TestMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mf := paths.NewMetaFile(dir, "drive-meta-2026-04-07.json")

	orig := &model.DocMeta{
		FileID:       "file-123",
		MimeType:     "application/vnd.google-apps.document",
		Title:        "My Doc",
		ModifiedTime: "2026-04-07T12:00:00Z",
		SyncedAt:     "2026-04-07T12:01:00Z",
		Tabs: []model.TabMeta{
			{ID: "tab-1", Title: "Main"},
			{ID: "tab-2", Title: "Notes"},
		},
		Sheets: []string{"Sheet1", "Sheet2"},
	}

	if err := SaveMeta(mf, orig); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	got, err := LoadMeta(mf)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}

	if got.FileID != orig.FileID {
		t.Errorf("FileID = %q, want %q", got.FileID, orig.FileID)
	}
	if got.Title != orig.Title {
		t.Errorf("Title = %q, want %q", got.Title, orig.Title)
	}
	if len(got.Tabs) != 2 {
		t.Fatalf("Tabs count = %d, want 2", len(got.Tabs))
	}
	if got.Tabs[0].Title != "Main" {
		t.Errorf("Tabs[0].Title = %q, want %q", got.Tabs[0].Title, "Main")
	}
	if len(got.Sheets) != 2 {
		t.Fatalf("Sheets count = %d, want 2", len(got.Sheets))
	}
}

func TestLoadMetaNonExistent(t *testing.T) {
	mf := paths.NewMetaFile(t.TempDir(), "nope.json")
	_, err := LoadMeta(mf)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestSaveMetaCleansUpStaleDriveMetaFiles(t *testing.T) {
	dir := t.TempDir()

	// Simulate a previously synced file with an older modifiedTime.
	oldPath := filepath.Join(dir, "drive-meta-2026-04-01.json")
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Save with a newer modifiedTime.
	mf := paths.NewMetaFile(dir, "drive-meta-2026-04-07.json")
	meta := &model.DocMeta{FileID: "f1", ModifiedTime: "2026-04-07T12:00:00Z"}
	if err := SaveMeta(mf, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	// New file exists, old file is gone.
	if _, err := os.Stat(mf.Path()); err != nil {
		t.Errorf("new meta file missing: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("stale meta file not cleaned up: err=%v", err)
	}
}

func TestSaveMetaLeavesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()

	// Create unrelated files that should not be touched.
	unrelated := []string{
		filepath.Join(dir, "Tab1.md"),
		filepath.Join(dir, "comments.jsonl"),
		filepath.Join(dir, "other.json"),
	}
	for _, p := range unrelated {
		if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mf := paths.NewMetaFile(dir, "drive-meta-2026-04-07.json")
	meta := &model.DocMeta{FileID: "f1"}
	if err := SaveMeta(mf, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	for _, p := range unrelated {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("unrelated file %s was removed: %v", p, err)
		}
	}
}
