package paths

import "path/filepath"

const (
	linearPlatform     = "linear-issues"
	linearIssuesSubdir = "issues"
)

// Linear returns a LinearDir for this account.
func (a AccountDir) Linear() LinearDir {
	return LinearDir{account: a}
}

// LinearDir represents the linear account directory: <base>/linear-issues/<workspace>/
type LinearDir struct {
	account AccountDir
}

// Path returns the linear directory path.
func (d LinearDir) Path() string {
	return d.account.Path()
}

// IssuesDir returns the path to the issues subdirectory.
func (d LinearDir) IssuesDir() string {
	return filepath.Join(d.Path(), linearIssuesSubdir)
}

// Issue returns a LinearIssueDir for the given identifier.
func (d LinearDir) Issue(identifier string) LinearIssueDir {
	return LinearIssueDir{linear: d, identifier: identifier}
}

// LinearIssueDir represents one issue's per-day-sharded directory:
//
//	<base>/linear-issues/<workspace>/issues/<identifier>/
//
// Each issue is its own append-only event stream sharded by UTC date,
// matching the messaging conversation pattern. The identifier is the
// human-readable Linear key (e.g. PROJ-123).
type LinearIssueDir struct {
	linear     LinearDir
	identifier string
}

// Path returns the per-issue directory path.
func (d LinearIssueDir) Path() string {
	return filepath.Join(d.linear.IssuesDir(), d.identifier)
}

// Identifier returns the issue identifier (e.g. "PROJ-123").
func (d LinearIssueDir) Identifier() string { return d.identifier }

// DateFile returns the path to a daily JSONL file under this issue.
func (d LinearIssueDir) DateFile(date string) LinearDateFile {
	return LinearDateFile(filepath.Join(d.Path(), date+FileExt))
}

// LinearDateFile is a path to a daily JSONL file in a Linear issue's
// event stream: <root>/linear-issues/<acct>/issues/<id>/YYYY-MM-DD.jsonl.
// Lines carry "updatedAt" (issue snapshots) and "createdAt" (comments).
type LinearDateFile string

// Path returns the file path as a string.
func (f LinearDateFile) Path() string { return string(f) }
func (LinearDateFile) logFile()       {}
func (LinearDateFile) dataFile()      {}

// Compile-time interface guard. LogFile transitively implies DataFile.
var _ LogFile = LinearDateFile("")
