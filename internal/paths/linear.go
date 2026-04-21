package paths

import (
	"path/filepath"
	"strings"
)

const (
	// LinearPlatform is the platform segment for Linear accounts under the
	// data root: <root>/linear-issues/<workspace>/...
	LinearPlatform = "linear-issues"

	linearIssuesSubdir = "issues"

	// LinearIssueGlobRg is the rg --glob pattern that matches Linear issue
	// JSONL files nested at <workspace>/issues/<identifier>.jsonl.
	LinearIssueGlobRg = "**/" + linearIssuesSubdir + "/*" + FileExt
)

// IsLinearIssueFile reports whether the given file path is a Linear issue
// JSONL file: <root>/linear/<workspace>/issues/<identifier>.jsonl.
func IsLinearIssueFile(path string) bool {
	if filepath.Ext(path) != FileExt {
		return false
	}
	if filepath.Base(filepath.Dir(path)) != linearIssuesSubdir {
		return false
	}
	sep := string(filepath.Separator)
	return strings.Contains(path, sep+LinearPlatform+sep)
}

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

// Compile-time interface guard.
var _ LogFile = IssueFile("")
