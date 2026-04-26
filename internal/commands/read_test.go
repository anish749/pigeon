package commands

import (
	"bytes"
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

// TestRunRead_JiraIssueRawPassthrough drives RunRead end-to-end for a Jira
// account: the issue file's bytes must reach stdout unchanged. The test
// captures stdout by swapping os.Stdout for a pipe (the same approach
// monitor_test.go uses).
func TestRunRead_JiraIssueRawPassthrough(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	acct := account.New(paths.JiraPlatform, "tubular")
	dataRoot := paths.NewDataRoot(root)
	issueFile := dataRoot.AccountFor(acct).Jira().Project("ENG").IssueFile("ENG-101")
	if err := os.MkdirAll(filepath.Dir(issueFile.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	want := `{"type":"jira-issue","key":"ENG-101","fields":{"summary":"Fix login"}}` + "\n" +
		`{"type":"jira-comment","id":"1","issueKey":"ENG-101","body":"shipped"}` + "\n"
	if err := os.WriteFile(issueFile.Path(), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	got := captureStdout(t, func() error {
		return RunRead(ReadParams{
			Platform: paths.JiraPlatform,
			Account:  "tubular",
			Contact:  "ENG-101",
		})
	})
	if got != want {
		t.Errorf("RunRead jira passthrough mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRunRead_JiraRejectsTimeFilters(t *testing.T) {
	t.Setenv("PIGEON_DATA_DIR", t.TempDir())
	for _, p := range []ReadParams{
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Date: "2026-04-26"},
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Last: 10},
		{Platform: paths.JiraPlatform, Account: "x", Contact: "ENG-1", Since: "1d"},
	} {
		err := RunRead(p)
		if err == nil || !strings.Contains(err.Error(), "does not support") {
			t.Errorf("RunRead with time filter should error, got %v (params=%+v)", err, p)
		}
	}
}

func TestRunRead_JiraIssueNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)
	// Account dir exists but no projects.
	acct := account.New(paths.JiraPlatform, "tubular")
	if err := os.MkdirAll(paths.NewDataRoot(root).AccountFor(acct).Jira().Path(), 0o755); err != nil {
		t.Fatal(err)
	}

	err := RunRead(ReadParams{Platform: paths.JiraPlatform, Account: "tubular", Contact: "ENG-101"})
	if err == nil {
		t.Fatal("RunRead should error for missing issue")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found', got %v", err)
	}
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns whatever
// fn wrote. Restores os.Stdout on cleanup.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	if err := fn(); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("fn: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	return string(<-done)
}
