package reader

import (
	"os"
	"testing"

	"github.com/anish749/pigeon/internal/paths"
)

func TestReadDriveDoc(t *testing.T) {
	dir := t.TempDir()
	driveDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Drive()
	fileDir := driveDir.File("q2-planning-abc123")
	if err := os.MkdirAll(fileDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, fileDir.MetaFile("2026-04-10").Path(),
		`{"fileId":"abc123","mimeType":"application/vnd.google-apps.document","title":"Q2 Planning","modifiedTime":"2026-04-10T14:22:00Z","syncedAt":"2026-04-10T15:00:00Z","tabs":[{"id":"t.0","title":"Tab 1"}]}`)

	writeFile(t, fileDir.TabFile("Tab 1").Path(), "# Q2 Planning\n\nThis is the roadmap.\n")

	writeFile(t, fileDir.CommentsFile().Path(),
		`{"type":"comment","id":"c1","content":"Looks good","author":{"displayName":"Alice"},"createdTime":"2026-04-10T12:00:00Z","resolved":false}
{"type":"comment","id":"c2","content":"Needs revision","author":{"displayName":"Bob"},"createdTime":"2026-04-10T13:00:00Z","resolved":false}
`)

	result, err := ReadDrive(fileDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Q2 Planning" {
		t.Errorf("title = %q, want %q", result.Title, "Q2 Planning")
	}
	if len(result.Tabs) != 1 {
		t.Fatalf("got %d tabs, want 1", len(result.Tabs))
	}
	if result.Tabs[0].Name != "Tab 1" {
		t.Errorf("tab name = %q, want %q", result.Tabs[0].Name, "Tab 1")
	}
	if len(result.Comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(result.Comments))
	}
}

func TestReadDriveSheet(t *testing.T) {
	dir := t.TempDir()
	driveDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Drive()
	fileDir := driveDir.File("budget-def456")
	if err := os.MkdirAll(fileDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, fileDir.MetaFile("2026-04-10").Path(),
		`{"fileId":"def456","mimeType":"application/vnd.google-apps.spreadsheet","title":"Budget","modifiedTime":"2026-04-10T14:22:00Z","syncedAt":"2026-04-10T15:00:00Z","sheets":["Summary","Revenue"]}`)

	writeFile(t, fileDir.SheetFile("Summary").Path(), "Category,Amount\nSalaries,100000\n")
	writeFile(t, fileDir.SheetFile("Revenue").Path(), "Month,Revenue\nJan,50000\n")

	result, err := ReadDrive(fileDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Budget" {
		t.Errorf("title = %q, want %q", result.Title, "Budget")
	}
	if len(result.Tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(result.Tabs))
	}
}

func TestFindDriveFile(t *testing.T) {
	dir := t.TempDir()
	driveDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Drive()

	// Create two doc directories with metadata.
	for _, slug := range []string{"q2-planning-abc", "budget-def"} {
		fileDir := driveDir.File(slug)
		if err := os.MkdirAll(fileDir.Path(), 0755); err != nil {
			t.Fatal(err)
		}
		title := "Q2 Planning"
		if slug == "budget-def" {
			title = "Budget"
		}
		writeFile(t, fileDir.MetaFile("2026-04-10").Path(),
			`{"fileId":"x","mimeType":"application/vnd.google-apps.document","title":"`+title+`","modifiedTime":"2026-04-10T14:22:00Z","syncedAt":"2026-04-10T15:00:00Z"}`)
	}

	// Exact match on slug.
	match, err := FindDriveFile(driveDir, "q2")
	if err != nil {
		t.Fatal(err)
	}
	if match.Path() != driveDir.File("q2-planning-abc").Path() {
		t.Errorf("match = %s, want q2-planning-abc", match.Path())
	}

	// Title match.
	match, err = FindDriveFile(driveDir, "Budget")
	if err != nil {
		t.Fatal(err)
	}
	if match.Path() != driveDir.File("budget-def").Path() {
		t.Errorf("match = %s, want budget-def", match.Path())
	}

	// No match.
	_, err = FindDriveFile(driveDir, "nonexistent")
	if err == nil {
		t.Error("expected error for no match")
	}
}
