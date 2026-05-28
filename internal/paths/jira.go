package paths

import "path/filepath"

const (
	// JiraPlatform is the on-disk platform name for Jira issue data.
	JiraPlatform = "jira"

	jiraIssuesSubdir = "issues"

	// jiraIssueFilename and jiraCommentsFilename are the two append-only
	// log files that live inside a per-issue directory. Issue snapshots
	// and comments have different schemas (jira-issue vs jira-comment
	// lines) so each gets its own file; readers and the maintenance
	// compaction pass dedup by id within each file.
	jiraIssueFilename    = "issue" + FileExt
	jiraCommentsFilename = "comments" + FileExt
)

// Jira returns a JiraDir for this account. The account slug is the
// pigeon-config `account` field, slugified by account.Account.NameSlug.
func (a AccountDir) Jira() JiraDir {
	return JiraDir{account: a}
}

// JiraDir represents the per-account Jira directory:
//
//	<base>/jira/<account-slug>/
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
//	<base>/jira/<account-slug>/<project-key>/
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

// Issue returns a JiraIssueDir for the given issue key.
func (d JiraProjectDir) Issue(key string) JiraIssueDir {
	return JiraIssueDir{project: d, key: key}
}

// SyncCursorsFile returns the per-project cursor file path:
// <base>/jira/<account>/<project>/.sync-cursors.yaml
//
// Each project has its own cursor because the JQL incremental query
// (`updated > "<cursor>"`) is project-scoped and cursors advance at
// different rates per project.
func (d JiraProjectDir) SyncCursorsFile() SyncCursorsFile {
	return SyncCursorsFile(filepath.Join(d.Path(), SyncCursorsFilename))
}

// JiraIssueDir represents one issue's append-only event store:
//
//	<base>/jira/<account>/<project>/issues/<KEY>/
//
// Each issue holds a separate issue.jsonl (snapshots) and comments.jsonl
// (comments). Splitting by line type keeps issue-level reads (status,
// assignee, priority) independent of comment-volume noise; the two
// schemas also differ enough that mixing them in one file made grepping
// awkward.
type JiraIssueDir struct {
	project JiraProjectDir
	key     string
}

// Path returns the per-issue directory path.
func (d JiraIssueDir) Path() string {
	return filepath.Join(d.project.IssuesDir(), d.key)
}

// Key returns the issue key (e.g. "ENG-142").
func (d JiraIssueDir) Key() string { return d.key }

// IssueFile returns the per-issue snapshot log: <KEY>/issue.jsonl.
func (d JiraIssueDir) IssueFile() JiraIssueFile {
	return JiraIssueFile(filepath.Join(d.Path(), jiraIssueFilename))
}

// CommentsFile returns the per-issue comments log: <KEY>/comments.jsonl.
func (d JiraIssueDir) CommentsFile() JiraCommentsFile {
	return JiraCommentsFile(filepath.Join(d.Path(), jiraCommentsFilename))
}

// JiraIssueFile is a path to a Jira issue's snapshot log:
// <base>/jira/<account>/<project>/issues/<KEY>/issue.jsonl. Lines carry
// fields.updated; one line per polled snapshot, deduped by id at read time.
//
// Distinct from Linear's per-issue logs so that compile-time routing prevents
// writing one platform's lines into the other's tree.
type JiraIssueFile string

// Path returns the file path as a string.
func (f JiraIssueFile) Path() string { return string(f) }
func (JiraIssueFile) logFile()       {}
func (JiraIssueFile) dataFile()      {}

// Key returns the issue key (e.g. "ENG-142") encoded by the file's
// per-issue directory. The layout fact "<KEY> is the per-issue dir name"
// stays in this registry so callers do not parse paths themselves.
func (f JiraIssueFile) Key() string { return filepath.Base(filepath.Dir(string(f))) }

// CommentsFile returns the sibling comments log path. issue.jsonl and
// comments.jsonl always live in the same per-issue directory.
func (f JiraIssueFile) CommentsFile() JiraCommentsFile {
	return JiraCommentsFile(filepath.Join(filepath.Dir(string(f)), jiraCommentsFilename))
}

// JiraIssueFileGlobs returns the rg --glob patterns that match every
// per-issue snapshot log under a JiraDir. Patterns are relative to
// JiraDir.Path(). The leading ** anchors the search to the layout
// (project key segment is opaque) without matching stray issue.jsonl
// files outside the issues/<KEY>/ shape.
func JiraIssueFileGlobs() []string {
	return []string{"**/" + jiraIssuesSubdir + "/*/" + jiraIssueFilename}
}

// JiraIssueFileGlobsForKey returns the rg --glob patterns that match the
// snapshot log for one specific issue key under a JiraDir. Patterns are
// relative to JiraDir.Path().
func JiraIssueFileGlobsForKey(key string) []string {
	return []string{"**/" + jiraIssuesSubdir + "/" + key + "/" + jiraIssueFilename}
}

// JiraCommentsFile is a path to a Jira issue's comments log:
// <base>/jira/<account>/<project>/issues/<KEY>/comments.jsonl. Lines carry
// the comment created/updated timestamps and the injected issueKey field
// that names the parent issue (so a comment line self-identifies its
// parent in grep output).
type JiraCommentsFile string

// Path returns the file path as a string.
func (f JiraCommentsFile) Path() string { return string(f) }
func (JiraCommentsFile) logFile()       {}
func (JiraCommentsFile) dataFile()      {}

// Compile-time interface guards. LogFile transitively implies DataFile.
var (
	_ LogFile = JiraIssueFile("")
	_ LogFile = JiraCommentsFile("")
)
