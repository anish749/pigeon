package paths

import (
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/account"
)

func TestJiraDirPath(t *testing.T) {
	root := NewDataRoot("/data")
	acct := account.New(JiraPlatform, "Acme Corp")
	jd := root.AccountFor(acct).Jira()

	want := "/data/jira/acme-corp"
	if jd.Path() != want {
		t.Errorf("JiraDir.Path() = %q, want %q", jd.Path(), want)
	}
}

func TestJiraProjectDirPath(t *testing.T) {
	root := NewDataRoot("/data")
	acct := account.New(JiraPlatform, "acme")
	pd := root.AccountFor(acct).Jira().Project("ENG")

	cases := []struct {
		got, want, name string
	}{
		{pd.Path(), "/data/jira/acme/ENG", "Path"},
		{pd.IssuesDir(), "/data/jira/acme/ENG/issues", "IssuesDir"},
		{pd.Issue("ENG-142").Path(), "/data/jira/acme/ENG/issues/ENG-142", "Issue.Path"},
		{pd.Issue("ENG-142").IssueFile().Path(), "/data/jira/acme/ENG/issues/ENG-142/issue.jsonl", "IssueFile"},
		{pd.Issue("ENG-142").CommentsFile().Path(), "/data/jira/acme/ENG/issues/ENG-142/comments.jsonl", "CommentsFile"},
		{pd.SyncCursorsFile().Path(), "/data/jira/acme/ENG/.sync-cursors.yaml", "SyncCursorsFile"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestJiraIssueFileImplementsLogFile(t *testing.T) {
	// Compile-time assertion: both per-issue files must implement LogFile so
	// that FSStore.AppendLine accepts them. Also asserted in jira.go via the
	// `var _ LogFile = ...` guards — the test below makes the requirement
	// visible to runtime tooling too.
	var _ LogFile = JiraIssueFile("")
	var _ DataFile = JiraIssueFile("")
	var _ LogFile = JiraCommentsFile("")
	var _ DataFile = JiraCommentsFile("")
}

func TestJiraProjectKeyCasePreserved(t *testing.T) {
	// Project keys are uppercase by Jira convention (ENG, OPS, KD-1699).
	// Pigeon must preserve case in directory names so paths match the
	// human-readable keys users see in Jira.
	pd := NewDataRoot("/d").AccountFor(account.New(JiraPlatform, "x")).Jira().Project("ENG")
	if !strings.HasSuffix(pd.Path(), "/ENG") {
		t.Errorf("project key case not preserved: %q", pd.Path())
	}
}

func TestJiraIssueFileKey(t *testing.T) {
	root := NewDataRoot("/data")
	f := root.AccountFor(account.New(JiraPlatform, "acme")).Jira().Project("ENG").Issue("ENG-142").IssueFile()
	if got := f.Key(); got != "ENG-142" {
		t.Errorf("Key() = %q, want %q", got, "ENG-142")
	}
}

func TestJiraIssueFileCommentsFile(t *testing.T) {
	root := NewDataRoot("/data")
	issue := root.AccountFor(account.New(JiraPlatform, "acme")).Jira().Project("ENG").Issue("ENG-142")
	if got := issue.IssueFile().CommentsFile().Path(); got != issue.CommentsFile().Path() {
		t.Errorf("IssueFile().CommentsFile() = %q, want %q", got, issue.CommentsFile().Path())
	}
}

func TestJiraIssueFileGlobs(t *testing.T) {
	got := JiraIssueFileGlobs()
	want := []string{"**/issues/*/issue.jsonl"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("JiraIssueFileGlobs() = %v, want %v", got, want)
	}
}

func TestJiraIssueFileGlobsForKey(t *testing.T) {
	got := JiraIssueFileGlobsForKey("ENG-142")
	want := []string{"**/issues/ENG-142/issue.jsonl"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("JiraIssueFileGlobsForKey() = %v, want %v", got, want)
	}
}
