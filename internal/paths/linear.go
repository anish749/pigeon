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

// LinearDir represents the linear account directory: <base>/linear/<workspace>/
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

// IssueFile returns the path to a specific issue's JSONL file.
func (d LinearDir) IssueFile(identifier string) IssueFile {
	return IssueFile(filepath.Join(d.IssuesDir(), identifier+FileExt))
}

// IssueFile is a path to a Linear issue's JSONL file.
type IssueFile string

// Path returns the file path as a string.
func (f IssueFile) Path() string { return string(f) }
func (IssueFile) logFile()       {}
func (IssueFile) dataFile()      {}

// Compile-time interface guard. LogFile transitively implies DataFile.
var _ LogFile = IssueFile("")
