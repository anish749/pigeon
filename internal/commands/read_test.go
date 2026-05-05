package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

// createConvDirs creates empty conversation directories under the store root.
func createConvDirs(t *testing.T, root paths.DataRoot, acct account.Account, names ...string) {
	t.Helper()
	for _, name := range names {
		dir := root.AccountFor(acct).Conversation(name).Path()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create conv dir %s: %v", name, err)
		}
	}
}

func TestFindConversations_SubstringMatchesBoth(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "@Alice Smith", "#general")

	// "@alice" is a substring of both "@alice" and "@Alice Smith"
	matches, err := findConversations(s, acct, "@alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_MultipleSubstring(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "@Alice Smith", "@mpdm-alice--bob-1")

	matches, err := findConversations(s, acct, "alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_CaseInsensitive(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@Alice", "#general")

	matches, err := findConversations(s, acct, "alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_NoMatch(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "#general")

	_, err := findConversations(s, acct, "bob", nil)
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestFindConversations_MatchesAlias(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "+14155551234_Alice", "#general")

	aliases := map[string][]string{
		"+14155551234_Alice": {"Mom"},
	}

	matches, err := findConversations(s, acct, "mom", aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
	if matches[0].displayName != "Mom" {
		t.Errorf("expected display name Mom, got %s", matches[0].displayName)
	}
}

func TestFindConversations_DisplayNameMatch(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "+14155551234_Alice", "+14155559876_Bob")

	matches, err := findConversations(s, acct, "Alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
	if matches[0].dirName != "+14155551234_Alice" {
		t.Errorf("expected +14155551234_Alice, got %s", matches[0].dirName)
	}
}

func TestFindConversations_NoDuplicateOnAliasAndDirName(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice")

	aliases := map[string][]string{
		"@alice": {"alice-wonderland"},
	}

	matches, err := findConversations(s, acct, "alice", aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (no duplicate from alias), got %d: %v", len(matches), convNames(matches))
	}
}

func convNames(matches []*conversation) []string {
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.dirName
	}
	return names
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written. Used to assert the output shape of commands that
// stream straight to stdout.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fnErr := fn()
	w.Close()
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read pipe: %v", readErr)
	}
	return string(out), fnErr
}

func TestRunReadJira_StreamsIssueAndComments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	acct := account.New(paths.JiraPlatform, "acme")
	issue := paths.NewDataRoot(root).AccountFor(acct).Jira().Project("ENG").Issue("ENG-101")

	if err := os.MkdirAll(issue.Path(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	issueLine := `{"type":"jira-issue","id":"10001","key":"ENG-101","fields":{"updated":"2026-04-01T09:00:00.000+0000"}}`
	commentLine := `{"type":"jira-comment","id":"50001","issueKey":"ENG-101","created":"2026-04-01T10:00:00.000+0000","updated":"2026-04-01T10:00:00.000+0000","body":"hello"}`
	if err := os.WriteFile(issue.IssueFile().Path(), []byte(issueLine+"\n"), 0o644); err != nil {
		t.Fatalf("write issue file: %v", err)
	}
	if err := os.WriteFile(issue.CommentsFile().Path(), []byte(commentLine+"\n"), 0o644); err != nil {
		t.Fatalf("write comments file: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return RunRead(ReadParams{
			Platform: paths.JiraPlatform,
			Account:  "acme",
			Contact:  "ENG-101",
		})
	})
	if err != nil {
		t.Fatalf("RunRead: %v", err)
	}
	if !strings.Contains(out, `"type":"jira-issue"`) {
		t.Errorf("output missing jira-issue line: %q", out)
	}
	if !strings.Contains(out, `"type":"jira-comment"`) {
		t.Errorf("output missing jira-comment line: %q", out)
	}
	// Issue line must come before comment line.
	if i, c := strings.Index(out, `"type":"jira-issue"`), strings.Index(out, `"type":"jira-comment"`); i > c {
		t.Errorf("jira-issue should appear before jira-comment, got issue@%d comment@%d", i, c)
	}
}

func TestRunReadJira_TolerateMissingComments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	acct := account.New(paths.JiraPlatform, "acme")
	issue := paths.NewDataRoot(root).AccountFor(acct).Jira().Project("ENG").Issue("ENG-101")

	if err := os.MkdirAll(issue.Path(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	issueLine := `{"type":"jira-issue","id":"10001","key":"ENG-101","fields":{"updated":"2026-04-01T09:00:00.000+0000"}}`
	if err := os.WriteFile(issue.IssueFile().Path(), []byte(issueLine+"\n"), 0o644); err != nil {
		t.Fatalf("write issue file: %v", err)
	}
	// no comments.jsonl

	out, err := captureStdout(t, func() error {
		return RunRead(ReadParams{
			Platform: paths.JiraPlatform,
			Account:  "acme",
			Contact:  "ENG-101",
		})
	})
	if err != nil {
		t.Fatalf("RunRead: %v", err)
	}
	if !strings.Contains(out, `"type":"jira-issue"`) {
		t.Errorf("output missing jira-issue line: %q", out)
	}
}

func TestRunReadJira_RejectsTimeFlags(t *testing.T) {
	t.Setenv("PIGEON_DATA_DIR", t.TempDir())

	for _, p := range []ReadParams{
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Date: "2026-04-01"},
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Last: 5},
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Since: "1d"},
	} {
		err := RunRead(p)
		if err == nil {
			t.Errorf("RunRead %+v: want error, got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "does not support") {
			t.Errorf("RunRead %+v: error = %v, want contains 'does not support'", p, err)
		}
	}
}

func TestRunReadJira_NotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	// Account dir exists but no matching issue.
	acct := account.New(paths.JiraPlatform, "acme")
	if err := os.MkdirAll(paths.NewDataRoot(root).AccountFor(acct).Jira().Path(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := RunRead(ReadParams{
		Platform: paths.JiraPlatform,
		Account:  "acme",
		Contact:  "ENG-999",
	})
	if err == nil {
		t.Fatal("RunRead: want error for missing issue, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("RunRead: error = %v, want contains 'not found'", err)
	}
}

func TestStreamModelLines_ParseError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := streamModelLines(bad, false)
	if err == nil {
		t.Fatal("streamModelLines: want error for bad JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parse line 1") {
		t.Errorf("error = %v, want contains 'parse line 1'", err)
	}
}

func TestStreamModelLines_TolerateMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.jsonl")
	if err := streamModelLines(missing, true); err != nil {
		t.Errorf("streamModelLines tolerateMissing=true: %v", err)
	}
	if err := streamModelLines(missing, false); err == nil {
		t.Error("streamModelLines tolerateMissing=false: want error, got nil")
	}
}
