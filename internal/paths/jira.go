package paths

import "path/filepath"

const (
	// JiraPlatform is the on-disk platform name for Jira issue data.
	JiraPlatform = "jira-issues"

	jiraIssuesSubdir = "issues"
)

// Jira returns a JiraDir for this account. The account slug is the
// pigeon-config `account` field, slugified by account.Account.NameSlug.
func (a AccountDir) Jira() JiraDir {
	return JiraDir{account: a}
}

// JiraDir represents the per-account Jira directory:
//
//	<base>/jira-issues/<account-slug>/
//
// One Atlassian site (one set of credentials) maps to one JiraDir.
// Multiple projects on that site live in JiraProjectDirs underneath.
type JiraDir struct {
	account AccountDir
}

// Path returns the Jira account directory path.
func (d JiraDir) Path() string { return d.account.Path() }

// Project returns a JiraProjectDir for the given project key.
func (d JiraDir) Project(key string) JiraProjectDir {
	return JiraProjectDir{jira: d, project: key}
}

// JiraProjectDir represents a single Jira project directory:
//
//	<base>/jira-issues/<account-slug>/<project-key>/
//
// Each project carries its own .sync-cursors.yaml and issues/ subdirectory.
// Project keys are case-preserved here (e.g. "ENG", "OPS") so that on-disk
// paths match the human-readable keys used in Jira.
type JiraProjectDir struct {
	jira    JiraDir
	project string
}

// Path returns the project directory path.
func (d JiraProjectDir) Path() string {
	return filepath.Join(d.jira.Path(), d.project)
}

// IssuesDir returns the path to the issues subdirectory.
func (d JiraProjectDir) IssuesDir() string {
	return filepath.Join(d.Path(), jiraIssuesSubdir)
}

// IssueFile returns the JSONL file path for a specific issue key.
// Example: <base>/jira-issues/<account>/ENG/issues/ENG-101.jsonl
func (d JiraProjectDir) IssueFile(key string) JiraIssueFile {
	return JiraIssueFile(filepath.Join(d.IssuesDir(), key+FileExt))
}

// SyncCursorsFile returns the per-project cursor file path:
// <base>/jira-issues/<account>/<project>/.sync-cursors.yaml
//
// Each project has its own cursor because the JQL incremental query
// (`updated > "<cursor>"`) is project-scoped and cursors advance at
// different rates per project.
func (d JiraProjectDir) SyncCursorsFile() SyncCursorsFile {
	return SyncCursorsFile(filepath.Join(d.Path(), SyncCursorsFilename))
}

// JiraIssueFile is a path to a Jira issue's JSONL file:
// <base>/jira-issues/<account>/<project>/issues/<KEY>.jsonl
//
// Distinct from Linear's IssueFile so that compile-time routing prevents
// writing one platform's lines into the other's tree.
type JiraIssueFile string

// Path returns the file path as a string.
func (f JiraIssueFile) Path() string { return string(f) }
func (JiraIssueFile) logFile()       {}
func (JiraIssueFile) dataFile()      {}

// Compile-time interface guard.
var _ LogFile = JiraIssueFile("")
