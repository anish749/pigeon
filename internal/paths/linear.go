package paths

import "path/filepath"

const (
	linearPlatform     = "linear"
	linearIssuesSubdir = "issues"

	// linearIssueFilename and linearCommentsFilename are the two append-only
	// log files that live inside a per-issue directory. Each polled snapshot
	// of an issue produces one issue line plus N comment lines; routing them
	// to separate files keeps issue-level reads (state, assignee, labels)
	// independent of comment-volume noise.
	linearIssueFilename    = "issue" + FileExt
	linearCommentsFilename = "comments" + FileExt
)

// Linear returns a LinearDir for this account.
func (a AccountDir) Linear() LinearDir {
	return LinearDir{account: a}
}

// LinearDir represents the linear account directory:
// <base>/linear/<workspace>/.
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

// LinearIssueDir represents one issue's append-only event store:
//
//	<base>/linear/<workspace>/issues/<identifier>/
//
// Each issue holds a separate issue.jsonl (snapshots) and comments.jsonl
// (comments). The poller writes to both files independently; readers
// dedup by ID across the directory at read time.
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

// IssueFile returns the per-issue snapshot log: <id>/issue.jsonl.
func (d LinearIssueDir) IssueFile() LinearIssueFile {
	return LinearIssueFile(filepath.Join(d.Path(), linearIssueFilename))
}

// CommentsFile returns the per-issue comments log: <id>/comments.jsonl.
func (d LinearIssueDir) CommentsFile() LinearCommentsFile {
	return LinearCommentsFile(filepath.Join(d.Path(), linearCommentsFilename))
}

// LinearIssueFile is a path to an issue's snapshot log:
// <root>/linear/<acct>/issues/<id>/issue.jsonl. Lines carry a top-level
// "updatedAt"; one line per polled snapshot, deduped by id at read time.
type LinearIssueFile string

// Path returns the file path as a string.
func (f LinearIssueFile) Path() string { return string(f) }
func (LinearIssueFile) logFile()       {}
func (LinearIssueFile) dataFile()      {}

// LinearCommentsFile is a path to an issue's comments log:
// <root>/linear/<acct>/issues/<id>/comments.jsonl. Lines carry a
// top-level "createdAt"; comments are appended on every poll and deduped by
// id at read time.
type LinearCommentsFile string

// Path returns the file path as a string.
func (f LinearCommentsFile) Path() string { return string(f) }
func (LinearCommentsFile) logFile()       {}
func (LinearCommentsFile) dataFile()      {}

// Compile-time interface guards. LogFile transitively implies DataFile.
var (
	_ LogFile = LinearIssueFile("")
	_ LogFile = LinearCommentsFile("")
)
