package filekinds

import (
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// linearIssueKind matches Linear issue files:
//
//	<root>/linear/<workspace>/issues/<identifier>.jsonl
//
// A single issue file interleaves "linear-issue" lines (carrying "updatedAt")
// with "linear-comment" lines (carrying "createdAt"). Scanning both fields
// surfaces the most recent activity on the issue regardless of which source
// produced it.
type linearIssueKind struct{}

func (linearIssueKind) Name() string { return "linear-issue" }

func (linearIssueKind) Match(path string) bool {
	return paths.IsLinearIssueFile(path)
}

func (linearIssueKind) LatestTs(path string) (time.Time, error) {
	return scanLatestTs(path, "updatedAt", "createdAt")
}
