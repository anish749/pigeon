package store

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// These tests verify the JSONL format's greppability by writing real files
// and running actual grep/wc commands against them.

func seedGrepData(t *testing.T) (*FSStore, account.Account) {
	t.Helper()
	s, acct := setup(t)

	lines := []modelv1.Line{
		msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U04ABCD", "hello world"),
		msgLine("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U04EFGH", "deploy is done"),
		{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: "M3", Ts: ts(2026, 3, 16, 9, 2, 0),
			Sender: "User", SenderID: "U04USER", Via: modelv1.ViaPigeonAsUser,
			Text: "looks great Bob!",
		}},
		reactLine(ts(2026, 3, 16, 9, 3, 0), "M1", "Bob", "U04EFGH", "thumbsup"),
		{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: "M4", Ts: ts(2026, 3, 16, 9, 4, 0),
			Sender: "Alice", SenderID: "U04ALICE", Via: modelv1.ViaToPigeon,
			Text: "hey pigeon, summarize this channel",
		}},
		{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: "M5", Ts: ts(2026, 3, 16, 9, 5, 0),
			Sender: "pigeon", SenderID: "U04BOT", Via: modelv1.ViaPigeonAsBot,
			Text: "sure, working on it",
		}},
		{Type: modelv1.LineEdit, Edit: &modelv1.EditLine{
			Ts: ts(2026, 3, 16, 9, 6, 0), MsgID: "M2",
			Sender: "Bob", SenderID: "U04EFGH", Text: "deploy is done (v2.1)",
		}},
		{Type: modelv1.LineDelete, Delete: &modelv1.DeleteLine{
			Ts: ts(2026, 3, 16, 9, 7, 0), MsgID: "M1",
			Sender: "Alice", SenderID: "U04ABCD",
		}},
		{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
			ID: "M6", Ts: ts(2026, 3, 16, 9, 8, 0),
			Sender: "Bob", SenderID: "U04EFGH",
			Text: "check this out",
			Attachments: []modelv1.Attachment{{ID: "F07T3", Type: "image/jpeg"}},
		}},
		{Type: modelv1.LineUnreaction, React: &modelv1.ReactLine{
			Ts: ts(2026, 3, 16, 9, 9, 0), MsgID: "M1",
			Sender: "Bob", SenderID: "U04EFGH", Emoji: "thumbsup", Remove: true,
		}},
	}

	for _, line := range lines {
		if err := s.Append(acct, "#general", line); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	return s, acct
}

func grepFile(t *testing.T, pattern, file string) []string {
	t.Helper()
	out, err := exec.Command("grep", pattern, file).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // no matches
		}
		t.Fatalf("grep %q %s: %v", pattern, file, err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	return lines
}

func wcLines(t *testing.T, file string) int {
	t.Helper()
	out, err := exec.Command("wc", "-l", file).Output()
	if err != nil {
		t.Fatalf("wc -l %s: %v", file, err)
	}
	var count int
	for _, c := range strings.TrimSpace(string(out)) {
		if c >= '0' && c <= '9' {
			count = count*10 + int(c-'0')
		} else {
			break
		}
	}
	return count
}

func dateFilePath(s *FSStore, acct account.Account) string {
	return s.convDir(acct, "#general").DateFile("2026-03-16")
}

func TestGrep_FindMessagesByName(t *testing.T) {
	s, acct := seedGrepData(t)
	// Plain name search — no JSON syntax needed
	matches := grepFile(t, "Alice", dateFilePath(s, acct))
	if len(matches) < 2 {
		t.Errorf("grep 'Alice' found %d lines, want >= 2", len(matches))
	}
}

func TestGrep_FindAllReactions(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "react", dateFilePath(s, acct))
	// Matches react + unreact lines
	if len(matches) != 2 {
		t.Errorf("grep 'react' found %d lines, want 2", len(matches))
	}
}

func TestGrep_FindReactionsToSpecificMessage(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "thumbsup", dateFilePath(s, acct))
	// Both react and unreact lines contain the emoji name
	if len(matches) != 2 {
		t.Errorf("grep 'thumbsup' found %d lines, want 2", len(matches))
	}
}

func TestGrep_FindAttachments(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "image/jpeg", dateFilePath(s, acct))
	if len(matches) != 1 {
		t.Errorf("grep 'image/jpeg' found %d lines, want 1", len(matches))
	}
}

func TestGrep_FindEdits(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, `"edit"`, dateFilePath(s, acct))
	if len(matches) != 1 {
		t.Errorf("grep found %d lines, want 1", len(matches))
	}
}

func TestGrep_FindDeletes(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, `"delete"`, dateFilePath(s, acct))
	if len(matches) != 1 {
		t.Errorf("grep found %d lines, want 1", len(matches))
	}
}

func TestGrep_FindPigeonMessages(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "pigeon-as", dateFilePath(s, acct))
	if len(matches) != 2 {
		t.Errorf("grep 'pigeon-as' found %d lines, want 2 (as-user + as-bot)", len(matches))
	}
}

func TestGrep_FindToPigeon(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "to-pigeon", dateFilePath(s, acct))
	if len(matches) != 1 {
		t.Errorf("grep 'to-pigeon' found %d lines, want 1", len(matches))
	}
}

func TestGrep_FindAllVia(t *testing.T) {
	s, acct := seedGrepData(t)
	// All three via values contain "pigeon"; M5 has both via and sender "pigeon" on one line
	matches := grepFile(t, "pigeon", dateFilePath(s, acct))
	if len(matches) != 3 {
		t.Errorf("grep 'pigeon' found %d lines, want 3", len(matches))
	}
}

func TestGrep_FindByContent(t *testing.T) {
	s, acct := seedGrepData(t)
	matches := grepFile(t, "deploy", dateFilePath(s, acct))
	// Matches both the original message and the edit line
	if len(matches) != 2 {
		t.Errorf("grep 'deploy' found %d lines, want 2 (message + edit)", len(matches))
	}
}

func TestWc_CountsTotalEvents(t *testing.T) {
	s, acct := seedGrepData(t)
	count := wcLines(t, dateFilePath(s, acct))
	if count != 10 {
		t.Errorf("wc -l = %d, want 10 (6 msgs + 1 react + 1 unreact + 1 edit + 1 delete)", count)
	}
}

func TestGrep_CountMessagesOnly(t *testing.T) {
	s, acct := seedGrepData(t)
	// "msg" alone also appears in react/edit/delete lines (the "msg" target field),
	// so we match the type field specifically
	matches := grepFile(t, `"type":"msg"`, dateFilePath(s, acct))
	if len(matches) != 6 {
		t.Errorf("grep found %d lines, want 6 messages", len(matches))
	}
}

func TestGrep_NoNewlinesInMessages(t *testing.T) {
	s, acct := setup(t)

	// Write a message with newlines in the text
	m := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "NL1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1",
			Text: "line one\nline two\nline three",
		},
	}
	s.Append(acct, "#general", m)

	file := s.convDir(acct, "#general").DateFile("2026-03-16")
	count := wcLines(t, file)
	if count != 1 {
		t.Errorf("wc -l = %d, want 1 (multiline message should be one line)", count)
	}

	// grep should find the message on a single line
	matches := grepFile(t, "line one", file)
	if len(matches) != 1 {
		t.Errorf("grep 'line one' found %d lines, want 1", len(matches))
	}
	// Same line should contain "line two" (JSON escapes \n, still on same grep match)
	if len(matches) == 1 && !strings.Contains(matches[0], `line two`) {
		t.Error("multiline message not on single line")
	}
}

func TestGrep_SortOnTimestampWorks(t *testing.T) {
	s, acct := setup(t)

	// Write messages out of order
	s.Append(acct, "#general", msgLine("M2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "second"))
	s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "first"))

	file := s.convDir(acct, "#general").DateFile("2026-03-16")

	// sort on the file should produce chronological order
	// (JSON with "ts" field in ISO 8601 sorts lexicographically)
	out, err := exec.Command("sort", file).Output()
	if err != nil {
		t.Fatalf("sort: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("sort produced %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "first") {
		t.Errorf("sort: first line should contain 'first', got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "second") {
		t.Errorf("sort: second line should contain 'second', got: %s", lines[1])
	}
}
