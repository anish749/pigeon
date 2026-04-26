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
	acctDir := root.AccountFor(account.New("linear-issues", "test-ws"))
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
		date, err := dateOf(issue["updatedAt"].(string))
		if err != nil {
			t.Fatalf("dateOf: %v", err)
		}
		if err := s.AppendLine(linearDir.Issue(identifier).DateFile(date), line); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	for _, issue := range issues {
		identifier := issue["identifier"].(string)
		date, _ := dateOf(issue["updatedAt"].(string))
		path := linearDir.Issue(identifier).DateFile(date).Path()
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
	acctDir := root.AccountFor(account.New("linear-issues", "my-team"))
	s := store.NewFSStore(root)
	linearDir := acctDir.Linear()

	issue := map[string]any{
		"id":         "uuid-1",
		"identifier": "ENG-99",
		"updatedAt":  "2026-04-01T00:00:00Z",
	}
	line, err := issueToLine(issue)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(linearDir.Issue("ENG-99").DateFile("2026-04-01"), line); err != nil {
		t.Fatal(err)
	}

	// Verify the directory structure:
	// linear-issues/my-team/issues/ENG-99/2026-04-01.jsonl
	wantPath := filepath.Join(tmpDir, "linear-issues", "my-team", "issues", "ENG-99", "2026-04-01.jsonl")
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected file at %s: %v", wantPath, err)
	}
}

func TestWriteIssuesDedup(t *testing.T) {
	tmpDir := t.TempDir()
	root := paths.NewDataRoot(tmpDir)
	acctDir := root.AccountFor(account.New("linear-issues", "test-ws"))
	s := store.NewFSStore(root)
	linearDir := acctDir.Linear()

	// Two snapshots for the same issue land in two different date files
	// because each snapshot is keyed by its own updatedAt date. Read-time
	// dedup spans all date files for the issue.
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
		date, err := dateOf(updatedAt)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AppendLine(linearDir.Issue("ENG-50").DateFile(date), line); err != nil {
			t.Fatal(err)
		}
	}

	for _, date := range []string{"2026-04-01", "2026-04-02"} {
		data, err := os.ReadFile(linearDir.Issue("ENG-50").DateFile(date).Path())
		if err != nil {
			t.Fatalf("read %s: %v", date, err)
		}
		lines := splitNonEmpty(string(data))
		if len(lines) != 1 {
			t.Errorf("%s: got %d lines, want 1", date, len(lines))
		}
		parsed, err := modelv1.Parse(lines[0])
		if err != nil {
			t.Fatalf("Parse %s: %v", date, err)
		}
		id, ok := parsed.ID()
		if !ok || id != "uuid-1" {
			t.Errorf("%s: ID = %q, ok = %v", date, id, ok)
		}
	}
}

func TestDateOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-04-07T10:00:00Z", "2026-04-07"},
		{"2026-04-07T23:59:59-08:00", "2026-04-08"}, // crosses midnight UTC forward
		{"2026-04-07T00:30:00+05:30", "2026-04-06"}, // crosses midnight UTC backward
	}
	for _, c := range cases {
		got, err := dateOf(c.in)
		if err != nil {
			t.Errorf("dateOf(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("dateOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := dateOf("not a date"); err == nil {
		t.Error("dateOf with invalid input should error")
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
