package read

import (
	"fmt"
	"os"

	"github.com/anish749/pigeon/internal/paths"
)

// ListJiraIssues returns one paths.JiraIssueFile per issue stored under
// the Jira account, discovered via a single rg --files invocation across
// the project subdirectories. Layout knowledge (the glob shape) lives in
// the paths registry; this function only routes results back into typed
// values.
//
// Returns nil (not an error) when the account directory does not exist
// yet (first run before any sync) or contains no issues.
func ListJiraIssues(jd paths.JiraDir) ([]paths.JiraIssueFile, error) {
	if _, err := os.Stat(jd.Path()); os.IsNotExist(err) {
		return nil, nil
	}
	matches, err := GlobFiles(jd.Path(), paths.JiraIssueFileGlobs())
	if err != nil {
		return nil, fmt.Errorf("list jira issues in %s: %w", jd.Path(), err)
	}
	out := make([]paths.JiraIssueFile, 0, len(matches))
	for _, p := range matches {
		f, ok := paths.Classify(p).(paths.JiraIssueFile)
		if !ok {
			// Glob discovery and Classify dispatch should agree by
			// construction. A mismatch means the registry grew a new
			// shape that overlaps the issue glob — surface loud.
			return nil, fmt.Errorf("classify %s: did not resolve to JiraIssueFile", p)
		}
		out = append(out, f)
	}
	return out, nil
}

// FindJiraIssue locates the snapshot log for one specific issue key under
// the Jira account. Returns an error when the issue is not found, when
// more than one project contains the same key (impossible in real Jira —
// keys are globally unique within a site — but guarded so the contract
// stays explicit), or when discovery fails.
func FindJiraIssue(jd paths.JiraDir, key string) (paths.JiraIssueFile, error) {
	if _, err := os.Stat(jd.Path()); os.IsNotExist(err) {
		return "", fmt.Errorf("jira issue %s not found in %s", key, jd.Path())
	}
	matches, err := GlobFiles(jd.Path(), paths.JiraIssueFileGlobsForKey(key))
	if err != nil {
		return "", fmt.Errorf("find jira issue %s in %s: %w", key, jd.Path(), err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("jira issue %s not found in %s", key, jd.Path())
	case 1:
		f, ok := paths.Classify(matches[0]).(paths.JiraIssueFile)
		if !ok {
			return "", fmt.Errorf("classify %s: did not resolve to JiraIssueFile", matches[0])
		}
		return f, nil
	default:
		return "", fmt.Errorf("ambiguous jira issue %s in %s: %d files match", key, jd.Path(), len(matches))
	}
}
