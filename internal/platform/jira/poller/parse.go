package poller

import (
	"encoding/json"
	"fmt"
	"log/slog"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// splitIssueRaw takes the raw HTTP body returned by client.GetIssueRaw and
// splits it into one issue line plus N comment lines, where:
//
//   - The issue line preserves every top-level field of the response except
//     fields.comment.comments (the array is removed; total / maxResults /
//     startAt stay so callers can detect comment truncation).
//   - Each comment line is the per-comment object from the lifted array,
//     plus an injected issueKey field that names its parent.
//
// The injected issueKey is the load-bearing departure from Linear's comment
// shape: a Jira comment line, in isolation, must self-identify its parent so
// that grep filters like `rg '"issueKey":"ENG-142"'` work without knowing
// the file path.
//
// The third return value is the issue's fields.updated timestamp, used by
// the caller to advance the cursor.
func splitIssueRaw(key string, raw []byte) (modelv1.Line, []modelv1.Line, string, error) {
	var serialized map[string]any
	if err := json.Unmarshal(raw, &serialized); err != nil {
		return modelv1.Line{}, nil, "", fmt.Errorf("unmarshal issue body: %w", err)
	}
	var runtime jira.Issue
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return modelv1.Line{}, nil, "", fmt.Errorf("unmarshal issue runtime: %w", err)
	}

	commentMaps := liftComments(key, serialized)

	issueLine := modelv1.Line{
		Type: modelv1.LineJiraIssue,
		JiraIssue: &modelv1.JiraIssue{
			Runtime:    runtime,
			Serialized: serialized,
		},
	}

	commentLines := make([]modelv1.Line, 0, len(commentMaps))
	for _, cm := range commentMaps {
		// Inject parent key so the comment line self-identifies its issue.
		// Mutating the map in place is fine — these are owned by us.
		cm["issueKey"] = key

		// Build the runtime by re-marshaling the (now-augmented) map.
		// json.Decoder is more direct than re-encoding through a struct,
		// but unmarshal twice is simpler and the maps are small.
		b, err := json.Marshal(cm)
		if err != nil {
			return modelv1.Line{}, nil, "", fmt.Errorf("re-marshal comment for %s: %w", key, err)
		}
		var cr modelv1.JiraCommentRuntime
		if err := json.Unmarshal(b, &cr); err != nil {
			return modelv1.Line{}, nil, "", fmt.Errorf("comment runtime for %s: %w", key, err)
		}
		commentLines = append(commentLines, modelv1.Line{
			Type: modelv1.LineJiraComment,
			JiraComment: &modelv1.JiraComment{
				Runtime:    cr,
				Serialized: cm,
			},
		})
	}

	return issueLine, commentLines, runtime.Fields.Updated, nil
}

// liftComments removes the fields.comment.comments[] array from the
// serialized issue map and returns it. Sibling fields under
// fields.comment (total, maxResults, startAt) are preserved on the issue
// line so callers can detect comment truncation. Returns nil if no
// comments are present.
//
// Logs a warning when a key exists but holds the wrong shape — that is
// the failure mode where we'd otherwise silently drop comment data
// returned by a future Jira API change. Absent keys are not logged
// (a brand-new issue may have no fields.comment at all).
func liftComments(key string, serialized map[string]any) []map[string]any {
	fields, ok := requireMap(serialized, "fields", key)
	if !ok {
		// "fields" should always be present on a real Jira issue body.
		// If it's absent, log loudly because the issue line will go to
		// disk but we couldn't extract any comments.
		slog.Warn("jira issue body missing top-level fields object", "issue", key)
		return nil
	}
	cmt, ok := requireMap(fields, "comment", key)
	if !ok {
		// comment block legitimately absent on some queries / older
		// installations. Don't warn; just return.
		return nil
	}
	rawList, present := cmt["comments"]
	if !present {
		return nil
	}
	delete(cmt, "comments")
	list, ok := rawList.([]any)
	if !ok {
		slog.Warn("jira issue fields.comment.comments has unexpected shape, dropping",
			"issue", key, "got_type", fmt.Sprintf("%T", rawList))
		return nil
	}

	out := make([]map[string]any, 0, len(list))
	for i, c := range list {
		m, ok := c.(map[string]any)
		if !ok {
			slog.Warn("jira comment entry has unexpected shape, dropping",
				"issue", key, "index", i, "got_type", fmt.Sprintf("%T", c))
			continue
		}
		out = append(out, m)
	}
	return out
}

// requireMap fetches a nested map from m by key. Returns (nil, false) if
// the key is absent, and logs+returns (nil, false) if the key is present
// but the value is not a map[string]any (i.e. real schema drift, not
// just an empty issue).
func requireMap(m map[string]any, mapKey, issueKey string) (map[string]any, bool) {
	raw, present := m[mapKey]
	if !present {
		return nil, false
	}
	out, ok := raw.(map[string]any)
	if !ok {
		slog.Warn("jira issue field has unexpected shape, dropping",
			"issue", issueKey, "field", mapKey, "got_type", fmt.Sprintf("%T", raw))
		return nil, false
	}
	return out, true
}
