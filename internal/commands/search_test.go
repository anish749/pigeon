package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// TestGrepFallback_NoColorFlag verifies that captureGrepFallback does not
// use --no-color (which is invalid on macOS BSD grep). The correct flag is
// --color=never.
//
// Bug: search.go uses `grep --no-color` which fails on macOS BSD grep with
// exit code 2 (usage error), making the grep fallback completely broken.
func TestGrepFallback_NoColorFlag(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available")
	}

	dir := t.TempDir()
	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"),
	})

	// captureGrepFallback should find the match without erroring.
	// On macOS, --no-color causes grep to exit 2, which surfaces as a non-nil error.
	output, err := captureGrepFallback("deploy", dir, []string{"*.txt"}, 0)
	if err != nil {
		t.Fatalf("captureGrepFallback returned error: %v (likely --no-color is invalid on this grep)", err)
	}
	if len(output) == 0 {
		t.Error("captureGrepFallback returned empty output, expected a match")
	}
}

// setupThreadFixture creates a directory structure matching the pigeon layout
// with both date files and thread files nested under conversations:
//
//	<root>/slack/acme/#general/2026-03-16.txt        (channel messages)
//	<root>/slack/acme/#general/threads/1742100000.txt (thread messages)
func setupThreadFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Channel message
	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread parent about deploy"),
	})

	// Thread file: nested at <conversation>/threads/<ts>.txt
	writeTestJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "threads", "1742100000.txt"), []modelv1.Line{
		testMsg("M1", testTs(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread parent about deploy"),
		testMsg("R1", testTs(2026, 3, 16, 9, 5, 0), "Bob", "U2", "deploy reply in thread"),
	})

	return dir
}

// TestCaptureRg_ThreadGlob verifies that captureRg finds messages in thread
// files when the includes list contains the threads glob.
//
// Bug: the glob "threads/*.txt" only matches if threads/ is a direct child of
// the search root. But thread files live at <conv>/threads/<ts>.txt, so the
// correct glob is "**/threads/*.txt".
func TestCaptureRg_ThreadGlob(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg not available")
	}

	dir := setupThreadFixture(t)

	// Search with the thread glob — this should find thread file matches.
	includes := []string{"threads/*.txt"}
	output, err := captureRg(rgPath, "deploy", dir, includes, 0)
	if err != nil {
		t.Fatalf("captureRg: %v", err)
	}
	if len(output) == 0 {
		t.Error("captureRg with threads/*.txt glob returned no output; thread files were not searched")
	}
}

// TestCaptureGrepFallback_ThreadGlob verifies that captureGrepFallback finds
// messages in thread files when the includes list contains the threads glob.
//
// Bug: grep --include 'threads/*.txt' matches against basenames only. Since no
// basename contains a slash, this pattern never matches thread files.
func TestCaptureGrepFallback_ThreadGlob(t *testing.T) {
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available")
	}

	dir := setupThreadFixture(t)

	// Search with only the thread glob — should find thread file matches.
	includes := []string{"threads/*.txt"}
	output, err := captureGrepFallback("deploy", dir, includes, 0)
	if err != nil {
		t.Fatalf("captureGrepFallback: %v", err)
	}
	if len(output) == 0 {
		t.Error("captureGrepFallback with threads/*.txt glob returned no output; thread files were not searched")
	}
}

// TestFileIncludes_ThreadGlob verifies that fileIncludes produces a thread
// glob that actually matches nested thread files when used with rg.
func TestFileIncludes_ThreadGlob(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg not available")
	}

	dir := setupThreadFixture(t)

	// fileIncludes with a --since that covers our test date
	includes, err := fileIncludes(dir, "30d")
	if err != nil {
		t.Fatalf("fileIncludes: %v", err)
	}

	// Verify that includes contains a thread glob
	hasThreadGlob := false
	for _, inc := range includes {
		if inc == "threads/*.txt" || inc == "**/threads/*.txt" {
			hasThreadGlob = true
			break
		}
	}
	if !hasThreadGlob {
		t.Fatalf("fileIncludes did not return a thread glob, got: %v", includes)
	}

	// Use the includes with rg to search — thread results should appear
	output, err := captureRg(rgPath, "reply in thread", dir, includes, 0)
	if err != nil {
		t.Fatalf("captureRg: %v", err)
	}
	if len(output) == 0 {
		t.Error("rg with fileIncludes globs returned no output for a query that only matches in a thread file")
	}
}
