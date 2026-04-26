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

	want := "/data/jira-issues/acme-corp"
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
		{pd.Path(), "/data/jira-issues/acme/ENG", "Path"},
		{pd.IssuesDir(), "/data/jira-issues/acme/ENG/issues", "IssuesDir"},
		{pd.IssueFile("ENG-142").Path(), "/data/jira-issues/acme/ENG/issues/ENG-142.jsonl", "IssueFile"},
		{pd.SyncCursorsFile().Path(), "/data/jira-issues/acme/ENG/.sync-cursors.yaml", "SyncCursorsFile"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestJiraIssueFileImplementsLogFile(t *testing.T) {
	// Compile-time assertion: JiraIssueFile must implement LogFile so that
	// FSStore.AppendLine accepts it. This is also asserted in jira.go via
	// `var _ LogFile = JiraIssueFile("")` — the test below makes the
	// requirement visible to runtime tooling too.
	var _ LogFile = JiraIssueFile("")
	var _ DataFile = JiraIssueFile("")
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
