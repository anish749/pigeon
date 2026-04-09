package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func acctDir() AccountDir {
	return NewDataRoot("/tmp/test").Platform("gws").AccountFromSlug("user-at-gmail-com")
}

func TestGmailDir_Path(t *testing.T) {
	got := acctDir().Gmail().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gmail"
	if got != want {
		t.Errorf("Gmail().Path() = %q, want %q", got, want)
	}
}

func TestGmailDir_DateFile(t *testing.T) {
	got := acctDir().Gmail().DateFile("2024-01-15").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gmail/2024-01-15.jsonl"
	if got != want {
		t.Errorf("Gmail().DateFile() = %q, want %q", got, want)
	}
}

func TestCalendarDir_Path(t *testing.T) {
	got := acctDir().Calendar("primary").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gcalendar/primary"
	if got != want {
		t.Errorf("Calendar().Path() = %q, want %q", got, want)
	}
}

func TestCalendarDir_DateFile(t *testing.T) {
	got := acctDir().Calendar("primary").DateFile("2024-03-20").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gcalendar/primary/2024-03-20.jsonl"
	if got != want {
		t.Errorf("Calendar().DateFile() = %q, want %q", got, want)
	}
}

func TestDriveDir_Path(t *testing.T) {
	got := acctDir().Drive().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive"
	if got != want {
		t.Errorf("Drive().Path() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_Path(t *testing.T) {
	got := acctDir().Drive().File("meeting-notes-abc12345").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/meeting-notes-abc12345"
	if got != want {
		t.Errorf("DriveFile().Path() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_MetaFile(t *testing.T) {
	mf := acctDir().Drive().File("doc-abc").MetaFile("2026-04-07")

	wantPath := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/drive-meta-2026-04-07.json"
	if got := mf.Path(); got != wantPath {
		t.Errorf("Path() = %q, want %q", got, wantPath)
	}
	wantDir := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc"
	if got := mf.Dir(); got != wantDir {
		t.Errorf("Dir() = %q, want %q", got, wantDir)
	}
	wantName := "drive-meta-2026-04-07.json"
	if got := mf.Name(); got != wantName {
		t.Errorf("Name() = %q, want %q", got, wantName)
	}
}

func TestParseDriveMetaPath_Valid(t *testing.T) {
	path := "/tmp/test/gws/user/gdrive/doc-abc/drive-meta-2026-04-07.json"
	meta, ok, err := ParseDriveMetaPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if meta.Path() != path {
		t.Errorf("Path() = %q, want %q", meta.Path(), path)
	}
	if meta.Dir() != "/tmp/test/gws/user/gdrive/doc-abc" {
		t.Errorf("Dir() = %q", meta.Dir())
	}
}

func TestParseDriveMetaPath_NotAMetaFile(t *testing.T) {
	cases := []string{
		"/tmp/test/gws/user/gmail/2026-04-07.jsonl",        // wrong subdir
		"/tmp/test/gws/user/gdrive/doc-abc/Tab1.md",        // content file
		"/tmp/test/gws/user/gdrive/doc-abc/comments.jsonl", // comments file
		"/tmp/test/random/file.txt",                         // unrelated
	}
	for _, path := range cases {
		meta, ok, err := ParseDriveMetaPath(path)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", path, err)
		}
		if ok {
			t.Errorf("%s: ok = true, want false", path)
		}
		if meta != (DriveMetaFile{}) {
			t.Errorf("%s: meta != zero value", path)
		}
	}
}

func TestParseDriveMetaPath_MalformedDate(t *testing.T) {
	// Filename has the right prefix/extension but an unparseable date.
	cases := []string{
		"/tmp/test/gws/user/gdrive/doc-abc/drive-meta-not-a-date.json",
		"/tmp/test/gws/user/gdrive/doc-abc/drive-meta-2026-13-45.json",
		"/tmp/test/gws/user/gdrive/doc-abc/drive-meta-.json",
	}
	for _, path := range cases {
		_, ok, err := ParseDriveMetaPath(path)
		if err == nil {
			t.Errorf("%s: expected error for malformed date", path)
		}
		if !ok {
			t.Errorf("%s: ok = false, want true (shape matched)", path)
		}
	}
}

func TestDriveMetaFileGlobsSince(t *testing.T) {
	// 2-day window should produce patterns for today, yesterday, and 2 days ago.
	globs := DriveMetaFileGlobsSince(48 * time.Hour)
	if len(globs) != 3 {
		t.Errorf("got %d globs, want 3: %v", len(globs), globs)
	}
	// Each glob should be drive-meta-YYYY-MM-DD.json.
	for _, g := range globs {
		if !strings.HasPrefix(g, "drive-meta-") {
			t.Errorf("glob %q missing prefix", g)
		}
		if !strings.HasSuffix(g, ".json") {
			t.Errorf("glob %q missing suffix", g)
		}
	}
}

func TestDriveMetaFile_ContentFiles(t *testing.T) {
	// Use a temp dir because ContentFiles does a real os.ReadDir.
	dir := t.TempDir()
	root := NewDataRoot(dir)
	driveFile := root.Platform("gws").AccountFromSlug("user").Drive().File("doc-abc")
	meta := driveFile.MetaFile("2026-04-07")

	// Create the meta file and sibling content files.
	if err := os.MkdirAll(driveFile.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(meta.Path(), `{}`)
	write(driveFile.TabFile("Tab1").Path(), "content")
	write(driveFile.SheetFile("Sheet1").Path(), "a,b,c")
	write(driveFile.CommentsFile().Path(), `{}`)
	// Non-content file that should be ignored.
	write(filepath.Join(driveFile.Path(), "ignore.txt"), "ignored")

	content, err := meta.ContentFiles()
	if err != nil {
		t.Fatalf("ContentFiles: %v", err)
	}
	if len(content) != 3 {
		t.Errorf("got %d content files, want 3: %v", len(content), content)
	}
}

func TestDriveMetaFileZeroValue(t *testing.T) {
	var mf DriveMetaFile
	if mf.Path() != "" {
		t.Errorf("zero Path() = %q, want empty", mf.Path())
	}
	if mf.Dir() != "" {
		t.Errorf("zero Dir() = %q, want empty", mf.Dir())
	}
	if mf.Name() != "" {
		t.Errorf("zero Name() = %q, want empty", mf.Name())
	}
}

func TestDriveFileDir_CommentsFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").CommentsFile().Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/comments.jsonl"
	if got != want {
		t.Errorf("CommentsFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_TabFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").TabFile("Introduction").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/Introduction.md"
	if got != want {
		t.Errorf("TabFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_SheetFile(t *testing.T) {
	got := acctDir().Drive().File("sheet-xyz").SheetFile("Revenue").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/sheet-xyz/Revenue.csv"
	if got != want {
		t.Errorf("SheetFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_FormulaFile(t *testing.T) {
	got := acctDir().Drive().File("sheet-xyz").FormulaFile("Revenue").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/sheet-xyz/Revenue.formulas.csv"
	if got != want {
		t.Errorf("FormulaFile() = %q, want %q", got, want)
	}
}

func TestDriveFileDir_AttachmentFile(t *testing.T) {
	got := acctDir().Drive().File("doc-abc").AttachmentFile("image.png").Path()
	want := "/tmp/test/gws/user-at-gmail-com/gdrive/doc-abc/attachments/image.png"
	if got != want {
		t.Errorf("AttachmentFile() = %q, want %q", got, want)
	}
}

func TestDriveDir_FindFilesByID(t *testing.T) {
	drive := NewDataRoot(t.TempDir()).Platform("gws").AccountFromSlug("user").Drive()
	if err := os.MkdirAll(drive.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(drive.Path(), "hello-world-fileID123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(drive.Path(), "fileID456"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(drive.Path(), "other-doc-fileID789"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Slugged dir matches fileID.
	got, err := drive.FindFilesByID("fileID123")
	if err != nil {
		t.Fatalf("FindFilesByID(fileID123): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches for fileID123, want 1: %v", len(got), got)
	}
	if got[0].Path() != filepath.Join(drive.Path(), "hello-world-fileID123") {
		t.Errorf("match path = %q, want hello-world-fileID123", got[0].Path())
	}

	// Plain fileID dir matches fileID.
	got, err = drive.FindFilesByID("fileID456")
	if err != nil {
		t.Fatalf("FindFilesByID(fileID456): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d matches for fileID456, want 1: %v", len(got), got)
	}
	if got[0].Path() != filepath.Join(drive.Path(), "fileID456") {
		t.Errorf("match path = %q, want fileID456", got[0].Path())
	}

	// No match returns empty slice, no error.
	got, err = drive.FindFilesByID("missingFileID")
	if err != nil {
		t.Fatalf("FindFilesByID(missingFileID): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches for missingFileID, want 0: %v", len(got), got)
	}

	// Missing gdrive directory returns empty slice, no error.
	missingDrive := NewDataRoot(t.TempDir()).Platform("gws").AccountFromSlug("nonexistent").Drive()
	got, err = missingDrive.FindFilesByID("fileID123")
	if err != nil {
		t.Fatalf("FindFilesByID on missing dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d matches on missing dir, want 0: %v", len(got), got)
	}
}
