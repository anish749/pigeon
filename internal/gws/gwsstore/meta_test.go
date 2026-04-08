package gwsstore

import (
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

func TestMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.json")

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

	if err := SaveMeta(paths.MetaFile(path), orig); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}

	got, err := LoadMeta(paths.MetaFile(path))
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
	path := filepath.Join(t.TempDir(), "nope.json")
	_, err := LoadMeta(paths.MetaFile(path))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}
