package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/search"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func writeTestJSONL(t *testing.T, path string, lines []modelv1.Line) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		data, err := modelv1.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

func testMsg(id string, ts time.Time, sender, senderID, text string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: id, Ts: ts, Sender: sender, SenderID: senderID, Text: text,
		},
	}
}

func testTs(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

// setupThreadFixture creates a directory structure matching the pigeon layout
// with both date files and thread files nested under conversations:
//
//	<root>/slack/acme/#general/2026-03-16.jsonl        (channel messages)
//	<root>/slack/acme/#general/threads/1742100000.jsonl (thread messages)
func setupThreadFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread parent about deploy"),
	})

	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "threads", "1742100000.jsonl"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread parent about deploy"),
		testMsg("R1", testTs(2026, 3, 16, 9, 5, 0), "Bob", "U2", "deploy reply in thread"),
	})

	return dir
}

// TestGrepFallback_NoColorFlag verifies that the grep fallback does not
// use --no-color (which is invalid on macOS BSD grep).
func TestGrepFallback_NoColorFlag(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available")
	}

	dir := t.TempDir()
	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"),
	})

	output, err := search.Grep("deploy", dir, 0, 0)
	if err != nil {
		t.Fatalf("Grep returned error: %v", err)
	}
	if len(output) == 0 {
		t.Error("Grep returned empty output, expected a match")
	}
}

// TestGrep_ThreadGlob verifies that Grep finds messages in thread files.
func TestGrep_ThreadGlob(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}

	dir := setupThreadFixture(t)

	output, err := search.Grep("reply in thread", dir, 30*24*time.Hour, 0)
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(output) == 0 {
		t.Error("Grep returned no output for a query that only matches in a thread file")
	}
}

// TestFindFiles_IncludesThreads verifies that FindFiles returns thread files.
func TestFindFiles_IncludesThreads(t *testing.T) {
	dir := setupThreadFixture(t)

	files, err := search.FindFiles(dir, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("FindFiles: %v", err)
	}

	hasThread := false
	for _, f := range files {
		if filepath.Base(filepath.Dir(f)) == "threads" {
			hasThread = true
			break
		}
	}
	if !hasThread {
		t.Errorf("FindFiles did not return thread files, got: %v", files)
	}
}

// TestFindFiles_NoSince returns all files.
func TestFindFiles_NoSince(t *testing.T) {
	dir := setupThreadFixture(t)

	files, err := search.FindFiles(dir, 0)
	if err != nil {
		t.Fatalf("FindFiles: %v", err)
	}
	// Should have both the date file and the thread file.
	if len(files) < 2 {
		t.Errorf("FindFiles returned %d files, want at least 2", len(files))
	}
}
