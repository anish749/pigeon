package read

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("rg"); err != nil {
		fmt.Fprintln(os.Stderr, "rg (ripgrep) is required but not found on PATH")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// writeFile creates a file with the given content, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// setupFixture creates a pigeon data tree with date files and thread files.
func setupFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	today := time.Now().UTC().Format("2006-01-02")
	old := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")

	// Recent date file.
	writeFile(t,
		filepath.Join(dir, "slack", "acme", "#general", today+".jsonl"),
		`{"type":"msg","ts":"`+today+`T09:00:00Z","id":"M1","sender":"Alice","from":"U1","text":"hello"}`+"\n",
	)

	// Old date file.
	writeFile(t,
		filepath.Join(dir, "slack", "acme", "#archive", old+".jsonl"),
		`{"type":"msg","ts":"`+old+`T09:00:00Z","id":"M2","sender":"Bob","from":"U2","text":"old"}`+"\n",
	)

	// Thread file with a recent message.
	writeFile(t,
		filepath.Join(dir, "slack", "acme", "#general", "threads", "1742100000.jsonl"),
		`{"type":"msg","ts":"`+today+`T10:00:00Z","id":"R1","sender":"Bob","from":"U2","text":"thread reply"}`+"\n",
	)

	// Thread file with only old messages.
	writeFile(t,
		filepath.Join(dir, "slack", "acme", "#archive", "threads", "1700000000.jsonl"),
		`{"type":"msg","ts":"`+old+`T10:00:00Z","id":"R2","sender":"Eve","from":"U3","text":"old thread"}`+"\n",
	)

	return dir
}

func TestGlob_NoSince(t *testing.T) {
	dir := setupFixture(t)

	files, err := Glob(dir, 0)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// Should return all 4 files (2 date + 2 thread).
	if len(files) != 4 {
		t.Errorf("got %d files, want 4: %v", len(files), files)
	}

	// All paths should be absolute.
	for _, f := range files {
		if !filepath.IsAbs(f.Path()) {
			t.Errorf("path is not absolute: %s", f.Path())
		}
	}
}

func TestGlob_SinceFiltersDates(t *testing.T) {
	dir := setupFixture(t)

	files, err := Glob(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	// Should include today's date file and today's thread, but not the
	// old date file or old thread.
	for _, f := range files {
		base := filepath.Base(f.Path())
		if base == time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")+".jsonl" {
			t.Errorf("Glob returned old date file: %s", f.Path())
		}
		if base == "1700000000.jsonl" {
			t.Errorf("Glob returned old thread file: %s", f.Path())
		}
	}

	if len(files) == 0 {
		t.Error("Glob returned no files, expected at least today's files")
	}
}

func TestGlob_SinceIncludesRecentThread(t *testing.T) {
	dir := setupFixture(t)

	files, err := Glob(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}

	hasThread := false
	for _, f := range files {
		if filepath.Base(f.Path()) == "1742100000.jsonl" {
			hasThread = true
			break
		}
	}
	if !hasThread {
		t.Errorf("Glob did not return recent thread file, got: %v", files)
	}
}

// TestGlob_NoSinceClassifiesJiraIssues verifies the Glob → Classify pipeline
// types Jira issue files correctly when no --since window is in play. The
// --since-window discovery for issue files (Linear and Jira both) is
// tracked as a separate bug; this test only locks in the no-window case so
// `pigeon glob` and `pigeon grep` (without --since) surface Jira issues
// with the right typed kind, ready for the dispatcher in list_since.
func TestGlob_NoSinceClassifiesJiraIssues(t *testing.T) {
	dir := t.TempDir()
	root := paths.NewDataRoot(dir)
	acct := account.New(paths.JiraPlatform, "tubular")
	issue := root.AccountFor(acct).Jira().Project("ENG").IssueFile("ENG-101")
	writeFile(t, issue.Path(), `{"type":"jira-issue","key":"ENG-101"}`+"\n")

	files, err := Glob(dir, 0)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %v", len(files), files)
	}
	if _, ok := files[0].(paths.JiraIssueFile); !ok {
		t.Errorf("file %q classified as %T, want paths.JiraIssueFile", files[0].Path(), files[0])
	}
}

func TestGlob_MissingDir(t *testing.T) {
	_, err := Glob("/nonexistent/path", 0)
	if err == nil {
		t.Error("Glob on missing dir should return error")
	}
}

func TestGlob_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	files, err := Glob(dir, 0)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0", len(files))
	}
}
