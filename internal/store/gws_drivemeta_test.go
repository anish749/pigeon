package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// testDriveFileDir returns a DriveFileDir rooted at a temp directory for tests.
func testDriveFileDir(t *testing.T) paths.DriveFileDir {
	t.Helper()
	return paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("test").
		Drive().
		File("doc-abc")
}

func TestDriveMetaRoundTrip(t *testing.T) {
	fileDir := testDriveFileDir(t)
	mf := fileDir.MetaFile("2026-04-07")

	orig := &modelv1.DocMeta{
		FileID:       "file-123",
		MimeType:     "application/vnd.google-apps.document",
		Title:        "My Doc",
		ModifiedTime: "2026-04-07T12:00:00Z",
		SyncedAt:     "2026-04-07T12:01:00Z",
		Tabs: []modelv1.TabMeta{
			{ID: "tab-1", Title: "Main"},
			{ID: "tab-2", Title: "Notes"},
		},
		Sheets: []string{"Sheet1", "Sheet2"},
	}

	if err := SaveDriveMeta(mf, orig); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}

	got, err := LoadDriveMeta(mf)
	if err != nil {
		t.Fatalf("LoadDriveMeta: %v", err)
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

func TestLoadDriveMetaNonExistent(t *testing.T) {
	mf := testDriveFileDir(t).MetaFile("2026-04-07")
	_, err := LoadDriveMeta(mf)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestSaveDriveMetaCleansUpStaleFiles(t *testing.T) {
	fileDir := testDriveFileDir(t)

	// Create the drive file directory so we can seed a stale meta file.
	if err := os.MkdirAll(fileDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate a previously synced file with an older modifiedTime.
	oldPath := filepath.Join(fileDir.Path(), "drive-meta-2026-04-01.json")
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Save with a newer modifiedTime.
	mf := fileDir.MetaFile("2026-04-07")
	meta := &modelv1.DocMeta{FileID: "f1", ModifiedTime: "2026-04-07T12:00:00Z"}
	if err := SaveDriveMeta(mf, meta); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}

	// New file exists, old file is gone.
	if _, err := os.Stat(mf.Path()); err != nil {
		t.Errorf("new meta file missing: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("stale meta file not cleaned up: err=%v", err)
	}
}

func TestSaveDriveMetaLeavesUnrelatedFiles(t *testing.T) {
	fileDir := testDriveFileDir(t)
	if err := os.MkdirAll(fileDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create unrelated files that should not be touched.
	unrelated := []string{
		filepath.Join(fileDir.Path(), "Tab1.md"),
		filepath.Join(fileDir.Path(), "comments.jsonl"),
		filepath.Join(fileDir.Path(), "other.json"),
	}
	for _, p := range unrelated {
		if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mf := fileDir.MetaFile("2026-04-07")
	meta := &modelv1.DocMeta{FileID: "f1"}
	if err := SaveDriveMeta(mf, meta); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}

	for _, p := range unrelated {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("unrelated file %s was removed: %v", p, err)
		}
	}
}
