package filekinds

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// linearIssueKind matches Linear issue files:
//
//	<root>/linear-issues/<workspace>/issues/<identifier>.jsonl
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

func (linearIssueKind) Conversation(path, root string) Conversation {
	// Each Linear issue lives in exactly one JSONL file — no surrounding
	// directory. Dir is the file itself; Display drops ".jsonl" and the
	// redundant "issues/" path segment so the label reads as
	// "linear-issues/<workspace>/<identifier>".
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	// Fold "/issues/" out of the middle of the display path — the identifier
	// is already unique; repeating "issues" is noise. Keep the on-disk Dir
	// unchanged so callers can find the file.
	display := strings.Replace(rel, string(filepath.Separator)+"issues"+string(filepath.Separator), string(filepath.Separator), 1)
	return Conversation{Dir: path, Display: display}
}
