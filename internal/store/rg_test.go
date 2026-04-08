package store

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// --- rg (ripgrep) tests ---

func rgFile(t *testing.T, pattern, file string) []string {
	t.Helper()
	out, err := exec.Command("rg", "--no-filename", pattern, file).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // no matches
		}
		t.Fatalf("rg %q %s: %v", pattern, file, err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	return lines
}

func rgCount(t *testing.T, pattern, file string) int {
	t.Helper()
	out, err := exec.Command("rg", "--count", pattern, file).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0
		}
		t.Fatalf("rg --count %q %s: %v", pattern, file, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("rg --count parse: %v", err)
	}
	return n
}

func TestRg_FindByName(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := rgFile(t, "Alice", dateFilePath(s, acct))
	if len(matches) < 2 {
		t.Errorf("rg 'Alice' found %d lines, want >= 2", len(matches))
	}
}

func TestRg_FindByContent(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := rgFile(t, "deploy", dateFilePath(s, acct))
	if len(matches) != 2 {
		t.Errorf("rg 'deploy' found %d lines, want 2 (message + edit)", len(matches))
	}
}

func TestRg_FindReactions(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := rgFile(t, "thumbsup", dateFilePath(s, acct))
	if len(matches) != 2 {
		t.Errorf("rg 'thumbsup' found %d lines, want 2 (react + unreact)", len(matches))
	}
}

func TestRg_FindAttachments(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := rgFile(t, "image/jpeg", dateFilePath(s, acct))
	if len(matches) != 1 {
		t.Errorf("rg 'image/jpeg' found %d lines, want 1", len(matches))
	}
}

func TestRg_FindPigeonMessages(t *testing.T) {
	s, acct := seedGrepData(t)
	if n := rgCount(t, "pigeon-as", dateFilePath(s, acct)); n != 2 {
		t.Errorf("rg --count 'pigeon-as' = %d, want 2", n)
	}
}

func TestRg_FindToPigeon(t *testing.T) {
	s, acct := seedGrepData(t)
	if n := rgCount(t, "to-pigeon", dateFilePath(s, acct)); n != 1 {
		t.Errorf("rg --count 'to-pigeon' = %d, want 1", n)
	}
}

func TestRg_CountMessages(t *testing.T) {
	s, acct := seedGrepData(t)
	if n := rgCount(t, `"type":"msg"`, dateFilePath(s, acct)); n != 6 {
		t.Errorf("rg --count messages = %d, want 6", n)
	}
}

func TestRg_ContextAroundMatch(t *testing.T) {
	s, acct := seedGrepData(t)
	// rg -C 1 gives 1 line before and after each match
	out, err := exec.Command("rg", "--no-filename", "-C", "1", "deploy is done", dateFilePath(s, acct)).Output()
	if err != nil {
		t.Fatalf("rg -C 1: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	// Should have the match plus context lines (at least 2 lines: the match + neighbors)
	if len(lines) < 2 {
		t.Errorf("rg -C 1 returned %d lines, want >= 2 (match + context)", len(lines))
	}
	// Context lines should be valid JSON events
	for _, line := range lines {
		if line == "--" {
			continue // rg separator between groups
		}
		if _, err := modelv1.Parse(line); err != nil {
			t.Errorf("context line is not valid JSONL: %s", line)
		}
	}
}

func TestRg_NoNewlinesInMessages(t *testing.T) {
	s, acct := setup(t)
	m := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "NL1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1",
			Text: "line one\nline two\nline three",
		},
	}
	s.Append(acct, "#general", m)

	file := s.convDir(acct, "#general").DateFile("2026-03-16").Path()

	// rg should find all three "line X" substrings on the same single line
	matches := rgFile(t, "line one", file)
	if len(matches) != 1 {
		t.Errorf("rg 'line one' found %d lines, want 1", len(matches))
	}
	if len(matches) == 1 && !strings.Contains(matches[0], "line two") {
		t.Error("multiline message not on single line")
	}
}

// --- jq tests ---

func jqFile(t *testing.T, filter, file string) []string {
	t.Helper()
	out, err := exec.Command("jq", "-c", filter, file).Output()
	if err != nil {
		t.Fatalf("jq %q %s: %v\noutput: %s", filter, file, err, out)
	}
	result := strings.TrimRight(string(out), "\n")
	if result == "" {
		return nil
	}
	return strings.Split(result, "\n")
}

func jqRaw(t *testing.T, filter, file string) []string {
	t.Helper()
	out, err := exec.Command("jq", "-r", filter, file).Output()
	if err != nil {
		t.Fatalf("jq -r %q %s: %v", filter, file, err)
	}
	result := strings.TrimRight(string(out), "\n")
	if result == "" {
		return nil
	}
	return strings.Split(result, "\n")
}

func TestJq_SelectMessagesByType(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.type == "msg")`, dateFilePath(s, acct))
	if len(lines) != 6 {
		t.Errorf("jq select messages = %d, want 6", len(lines))
	}
}

func TestJq_SelectBySender(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.sender == "Alice")`, dateFilePath(s, acct))
	// Alice: M1 (msg), M4 (msg), delete line (sender Alice)
	if len(lines) != 3 {
		t.Errorf("jq select sender=Alice = %d, want 3", len(lines))
	}
}

func TestJq_SelectReactions(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.type == "react")`, dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq select reactions = %d, want 1", len(lines))
	}
}

func TestJq_SelectEdits(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.type == "edit")`, dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq select edits = %d, want 1", len(lines))
	}
}

func TestJq_SelectDeletes(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.type == "delete")`, dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq select deletes = %d, want 1", len(lines))
	}
}

func TestJq_SelectWithAttachments(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.attach != null)`, dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq select with attachments = %d, want 1", len(lines))
	}
}

func TestJq_SelectByVia(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.via != null)`, dateFilePath(s, acct))
	if len(lines) != 3 {
		t.Errorf("jq select via != null = %d, want 3", len(lines))
	}
}

func TestJq_SelectPigeonSent(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.via != null and (.via | startswith("pigeon-as")))`, dateFilePath(s, acct))
	if len(lines) != 2 {
		t.Errorf("jq select pigeon-sent = %d, want 2", len(lines))
	}
}

func TestJq_SelectToPigeon(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.via == "to-pigeon")`, dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq select to-pigeon = %d, want 1", len(lines))
	}
}

func TestJq_FullTextSearch(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t, `select(.text != null and (.text | contains("deploy")))`, dateFilePath(s, acct))
	if len(lines) != 2 {
		t.Errorf("jq text contains 'deploy' = %d, want 2", len(lines))
	}
}

func TestJq_ExtractMessageIDs(t *testing.T) {
	s, acct := seedGrepData(t)
	ids := jqRaw(t, `select(.type == "msg") | .id`, dateFilePath(s, acct))
	if len(ids) != 6 {
		t.Errorf("jq extract message IDs = %d, want 6", len(ids))
	}
	if ids[0] != "M1" {
		t.Errorf("first message ID = %q, want M1", ids[0])
	}
}

func TestJq_ExtractEditTargets(t *testing.T) {
	s, acct := seedGrepData(t)
	targets := jqRaw(t, `select(.type == "edit") | .msg`, dateFilePath(s, acct))
	if len(targets) != 1 || targets[0] != "M2" {
		t.Errorf("jq edit targets = %v, want [M2]", targets)
	}
}

func TestJq_FormatReadableOutput(t *testing.T) {
	s, acct := seedGrepData(t)
	// Format messages as "[HH:MM:SS] Sender: text" — the display use case
	lines := jqRaw(t,
		`select(.type == "msg") | "[" + .ts[11:19] + "] " + .sender + ": " + (.text // "")`,
		dateFilePath(s, acct))
	if len(lines) != 6 {
		t.Errorf("jq formatted output = %d lines, want 6", len(lines))
	}
	// First message should be formatted as "[09:00:00] Alice: hello world"
	want := "[09:00:00] Alice: hello world"
	if lines[0] != want {
		t.Errorf("formatted line 0 = %q, want %q", lines[0], want)
	}
}

func TestJq_CompoundQuery_SenderWithAttachments(t *testing.T) {
	s, acct := seedGrepData(t)
	lines := jqFile(t,
		`select(.sender == "Bob" and .attach != null)`,
		dateFilePath(s, acct))
	if len(lines) != 1 {
		t.Errorf("jq Bob+attachments = %d, want 1", len(lines))
	}
}

func TestJq_SortByTimestamp(t *testing.T) {
	s, acct := setup(t)

	// Write out of order
	s.Append(acct, "#general", msgLine("M2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "second"))
	s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "first"))

	file := s.convDir(acct, "#general").DateFile("2026-03-16").Path()

	// jq -s slurps all lines into an array, sort_by(.ts) sorts, then re-emit .id
	out, err := exec.Command("jq", "-r", "-s", `sort_by(.ts)[] | .id`, file).Output()
	if err != nil {
		t.Fatalf("jq sort_by: %v", err)
	}
	sorted := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(sorted) != 2 {
		t.Fatalf("jq sort produced %d lines, want 2", len(sorted))
	}
	if sorted[0] != "M1" || sorted[1] != "M2" {
		t.Errorf("jq sort_by(.ts): got %v, want [M1, M2]", sorted)
	}
}
