package read

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

// setupJiraFixture creates a Jira data tree with two projects, multiple
// issues per project, and the expected issue.jsonl + comments.jsonl pair
// per issue. Returns (data root, JiraDir for "acme" account).
func setupJiraFixture(t *testing.T) (string, paths.JiraDir) {
	t.Helper()
	dir := t.TempDir()
	acct := account.New(paths.JiraPlatform, "acme")
	jd := paths.NewDataRoot(dir).AccountFor(acct).Jira()

	for _, spec := range []struct {
		project, key string
	}{
		{"ENG", "ENG-101"},
		{"ENG", "ENG-142"},
		{"OPS", "OPS-7"},
	} {
		issue := jd.Project(spec.project).Issue(spec.key)
		writeFile(t, issue.IssueFile().Path(),
			`{"type":"jira-issue","id":"`+spec.key+`","key":"`+spec.key+`","fields":{"updated":"2026-04-01T09:00:00.000+0000"}}`+"\n",
		)
		writeFile(t, issue.CommentsFile().Path(),
			`{"type":"jira-comment","id":"1","issueKey":"`+spec.key+`","created":"2026-04-01T10:00:00.000+0000","updated":"2026-04-01T10:00:00.000+0000"}`+"\n",
		)
	}

	return dir, jd
}

func TestListJiraIssues(t *testing.T) {
	_, jd := setupJiraFixture(t)

	files, err := ListJiraIssues(jd)
	if err != nil {
		t.Fatalf("ListJiraIssues: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %v", len(files), files)
	}

	keys := make([]string, len(files))
	for i, f := range files {
		keys[i] = f.Key()
		if !strings.HasSuffix(f.Path(), "/issue.jsonl") {
			t.Errorf("path %q does not end in /issue.jsonl", f.Path())
		}
		if !filepath.IsAbs(f.Path()) {
			t.Errorf("path is not absolute: %s", f.Path())
		}
	}
	slices.Sort(keys)
	want := []string{"ENG-101", "ENG-142", "OPS-7"}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}

func TestListJiraIssues_EmptyAccount(t *testing.T) {
	dir := t.TempDir()
	acct := account.New(paths.JiraPlatform, "empty")
	jd := paths.NewDataRoot(dir).AccountFor(acct).Jira()

	files, err := ListJiraIssues(jd)
	if err != nil {
		t.Fatalf("ListJiraIssues: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0", len(files))
	}
}

func TestFindJiraIssue(t *testing.T) {
	_, jd := setupJiraFixture(t)

	f, err := FindJiraIssue(jd, "ENG-142")
	if err != nil {
		t.Fatalf("FindJiraIssue: %v", err)
	}
	if f.Key() != "ENG-142" {
		t.Errorf("Key() = %q, want %q", f.Key(), "ENG-142")
	}
	if !strings.HasSuffix(f.Path(), "/ENG/issues/ENG-142/issue.jsonl") {
		t.Errorf("path %q has unexpected shape", f.Path())
	}
}

func TestFindJiraIssue_NotFound(t *testing.T) {
	_, jd := setupJiraFixture(t)

	_, err := FindJiraIssue(jd, "ENG-999")
	if err == nil {
		t.Fatal("FindJiraIssue: want error for missing issue, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want contains 'not found'", err)
	}
}

func TestFindJiraIssue_AmbiguousAcrossProjects(t *testing.T) {
	// Two projects holding the same key — not realistic in a single Jira
	// site, but the contract guards against it explicitly.
	dir := t.TempDir()
	acct := account.New(paths.JiraPlatform, "dup")
	jd := paths.NewDataRoot(dir).AccountFor(acct).Jira()

	for _, project := range []string{"ENG", "OPS"} {
		issue := jd.Project(project).Issue("DUP-1")
		writeFile(t, issue.IssueFile().Path(),
			`{"type":"jira-issue","id":"DUP-1","key":"DUP-1"}`+"\n",
		)
	}

	_, err := FindJiraIssue(jd, "DUP-1")
	if err == nil {
		t.Fatal("FindJiraIssue: want error for ambiguous key, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %v, want contains 'ambiguous'", err)
	}
}
