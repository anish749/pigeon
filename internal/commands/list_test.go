package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

func TestListJiraAccount_PrintsKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	acct := account.New(paths.JiraPlatform, "acme")
	jd := paths.NewDataRoot(root).AccountFor(acct).Jira()

	for _, spec := range []struct{ project, key string }{
		{"ENG", "ENG-101"},
		{"OPS", "OPS-7"},
	} {
		issue := jd.Project(spec.project).Issue(spec.key)
		if err := os.MkdirAll(issue.Path(), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(issue.IssueFile().Path(), []byte(`{"type":"jira-issue","id":"`+spec.key+`","key":"`+spec.key+`"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	out, err := captureStdout(t, func() error { return listJiraAccount(acct) })
	if err != nil {
		t.Fatalf("listJiraAccount: %v", err)
	}
	for _, key := range []string{"ENG-101", "OPS-7"} {
		if !strings.Contains(out, key) {
			t.Errorf("output missing %q: %q", key, out)
		}
	}
}

func TestListJiraAccount_Empty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", root)

	acct := account.New(paths.JiraPlatform, "ghost")

	out, err := captureStdout(t, func() error { return listJiraAccount(acct) })
	if err != nil {
		t.Fatalf("listJiraAccount: %v", err)
	}
	if !strings.Contains(out, "No issues found") {
		t.Errorf("output missing 'No issues found': %q", out)
	}
}
