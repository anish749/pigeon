package poller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestIssueToLine(t *testing.T) {
	issue := map[string]any{
		"id":         "uuid-1",
		"identifier": "ENG-42",
		"updatedAt":  "2026-04-05T10:00:00Z",
		"title":      "Test issue",
	}

	line, err := issueToLine(issue)
	if err != nil {
		t.Fatalf("issueToLine: %v", err)
	}
	if line.Type != modelv1.LineLinearIssue {
		t.Errorf("Type = %q, want %q", line.Type, modelv1.LineLinearIssue)
	}
	if line.Issue.Runtime.ID != "uuid-1" {
		t.Errorf("Runtime.ID = %q, want %q", line.Issue.Runtime.ID, "uuid-1")
	}
	if line.Issue.Runtime.Identifier != "ENG-42" {
		t.Errorf("Runtime.Identifier = %q, want %q", line.Issue.Runtime.Identifier, "ENG-42")
	}
	if line.Issue.Serialized["title"] != "Test issue" {
		t.Errorf("Serialized[title] = %v", line.Issue.Serialized["title"])
	}
}

func TestCommentToLine(t *testing.T) {
	comment := map[string]any{
		"id":        "comment-1",
		"createdAt": "2026-04-08T14:00:00Z",
		"body":      "LGTM",
		"user":      map[string]any{"name": "Alice"},
	}

	line, err := commentToLine(comment)
	if err != nil {
		t.Fatalf("commentToLine: %v", err)
	}
	if line.Type != modelv1.LineLinearComment {
		t.Errorf("Type = %q, want %q", line.Type, modelv1.LineLinearComment)
	}
	if line.LinearComment.Runtime.ID != "comment-1" {
		t.Errorf("Runtime.ID = %q", line.LinearComment.Runtime.ID)
	}
	if line.LinearComment.Serialized["body"] != "LGTM" {
		t.Errorf("Serialized[body] = %v", line.LinearComment.Serialized["body"])
	}
}

func TestIssueToLineMissingFields(t *testing.T) {
	// An issue with no id/identifier should still marshal (fields are just empty).
	issue := map[string]any{
		"title": "No id issue",
	}
	line, err := issueToLine(issue)
	if err != nil {
		t.Fatalf("issueToLine: %v", err)
	}
	if line.Issue.Runtime.ID != "" {
		t.Errorf("Runtime.ID = %q, want empty", line.Issue.Runtime.ID)
	}
}

func TestWriteIssuesCreateFiles(t *testing.T) {
	tmpDir := t.TempDir()
	root := paths.NewDataRoot(tmpDir)
	acctDir := root.AccountFor(account.New("linear", "test-ws"))
	s := store.NewFSStore(root)
	linearDir := acctDir.Linear()

	issues := []map[string]any{
		{
			"id":         "uuid-1",
			"identifier": "ENG-10",
			"updatedAt":  "2026-04-01T10:00:00Z",
			"title":      "First issue",
		},
		{
			"id":         "uuid-2",
			"identifier": "ENG-11",
			"updatedAt":  "2026-04-02T10:00:00Z",
			"title":      "Second issue",
		},
	}

	for _, issue := range issues {
		line, err := issueToLine(issue)
		if err != nil {
			t.Fatalf("issueToLine: %v", err)
		}
		identifier := issue["identifier"].(string)
		if err := s.AppendLine(linearDir.Issue(identifier).IssueFile(), line); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	for _, issue := range issues {
		identifier := issue["identifier"].(string)
		path := linearDir.Issue(identifier).IssueFile().Path()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", identifier, err)
		}

		lines := splitNonEmpty(string(data))
		if len(lines) != 1 {
			t.Errorf("%s: got %d lines, want 1", identifier, len(lines))
		}

		parsed, err := modelv1.Parse(lines[0])
		if err != nil {
			t.Fatalf("Parse %s: %v", identifier, err)
		}
		if parsed.Type != modelv1.LineLinearIssue {
			t.Errorf("Type = %q", parsed.Type)
		}
		if parsed.Issue.Runtime.Identifier != identifier {
			t.Errorf("Identifier = %q, want %q", parsed.Issue.Runtime.Identifier, identifier)
		}
	}
}

func TestWriteIssuesDirectoryLayout(t *testing.T) {
	tmpDir := t.TempDir()
	root := paths.NewDataRoot(tmpDir)
	acctDir := root.AccountFor(account.New("linear", "my-team"))
	s := store.NewFSStore(root)
	linearDir := acctDir.Linear()

	issue := map[string]any{
		"id":         "uuid-1",
		"identifier": "ENG-99",
		"updatedAt":  "2026-04-01T00:00:00Z",
	}
	issueLine, err := issueToLine(issue)
	if err != nil {
		t.Fatal(err)
	}
	commentLine, err := commentToLine(map[string]any{
		"id":        "comment-1",
		"createdAt": "2026-04-01T11:00:00Z",
		"body":      "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(linearDir.Issue("ENG-99").IssueFile(), issueLine); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(linearDir.Issue("ENG-99").CommentsFile(), commentLine); err != nil {
		t.Fatal(err)
	}

	// Verify the directory structure:
	//   linear/my-team/issues/ENG-99/issue.jsonl
	//   linear/my-team/issues/ENG-99/comments.jsonl
	wantIssue := filepath.Join(tmpDir, "linear", "my-team", "issues", "ENG-99", "issue.jsonl")
	if _, err := os.Stat(wantIssue); err != nil {
		t.Errorf("expected issue file at %s: %v", wantIssue, err)
	}
	wantComments := filepath.Join(tmpDir, "linear", "my-team", "issues", "ENG-99", "comments.jsonl")
	if _, err := os.Stat(wantComments); err != nil {
		t.Errorf("expected comments file at %s: %v", wantComments, err)
	}
}

func TestWriteIssuesDedup(t *testing.T) {
	tmpDir := t.TempDir()
	root := paths.NewDataRoot(tmpDir)
	acctDir := root.AccountFor(account.New("linear", "test-ws"))
	s := store.NewFSStore(root)
	linearDir := acctDir.Linear()

	// Two poll cycles append two snapshots for the same issue. Read-time
	// dedup (and the maintenance pass) collapses them down to one.
	for i, updatedAt := range []string{"2026-04-01T00:00:00Z", "2026-04-02T00:00:00Z"} {
		issue := map[string]any{
			"id":         "uuid-1",
			"identifier": "ENG-50",
			"updatedAt":  updatedAt,
			"title":      "Version " + string(rune('1'+i)),
		}
		line, err := issueToLine(issue)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendLine(linearDir.Issue("ENG-50").IssueFile(), line); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(linearDir.Issue("ENG-50").IssueFile().Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := splitNonEmpty(string(data))
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2 (dedup happens at read/maintenance time)", len(lines))
	}
	for i, line := range lines {
		parsed, err := modelv1.Parse(line)
		if err != nil {
			t.Errorf("line %d: %v", i, err)
			continue
		}
		id, ok := parsed.ID()
		if !ok || id != "uuid-1" {
			t.Errorf("line %d: ID = %q, ok = %v", i, id, ok)
		}
	}
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
