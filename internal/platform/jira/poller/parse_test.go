package poller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// realIssueBody mimics what client.GetIssueRaw returns from Jira Cloud.
// Includes all the load-bearing structures: top-level id/key/self/fields,
// fields.updated/created, fields.comment with both metadata and comments[],
// and a custom field to confirm preservation.
const realIssueBody = `{
  "id": "10042",
  "key": "ENG-142",
  "self": "https://acme.atlassian.net/rest/api/3/issue/10042",
  "customfield_10016": 5,
  "fields": {
    "summary": "Fix login timeout",
    "updated": "2026-04-08T14:37:10.000+0000",
    "created": "2026-04-02T15:14:52.509+0000",
    "status": {"name": "In Progress"},
    "comment": {
      "total": 2,
      "maxResults": 1000,
      "startAt": 0,
      "comments": [
        {
          "id": "10501",
          "self": "https://acme.atlassian.net/rest/api/3/issue/10042/comment/10501",
          "author": {"displayName": "Bob", "accountId": "bob-id"},
          "body": "Looks good.",
          "created": "2026-04-08T14:04:31.883+0000",
          "updated": "2026-04-08T14:04:31.883+0000",
          "jsdPublic": true
        },
        {
          "id": "10502",
          "self": "https://acme.atlassian.net/rest/api/3/issue/10042/comment/10502",
          "author": {"displayName": "Alice"},
          "body": "Add a retry on timeout?",
          "created": "2026-04-08T14:30:00.000+0000",
          "updated": "2026-04-08T14:35:00.000+0000",
          "jsdPublic": false
        }
      ]
    }
  }
}`

func TestSplitIssueRawBasicShape(t *testing.T) {
	issueLine, commentLines, updated, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	if issueLine.Type != modelv1.LineJiraIssue {
		t.Errorf("issue Type = %q, want %q", issueLine.Type, modelv1.LineJiraIssue)
	}
	if len(commentLines) != 2 {
		t.Fatalf("got %d comment lines, want 2", len(commentLines))
	}
	if updated != "2026-04-08T14:37:10.000+0000" {
		t.Errorf("updated = %q", updated)
	}
}

func TestSplitIssueRawStripsCommentsArray(t *testing.T) {
	// fields.comment.comments[] should be removed from the issue line, but
	// total/maxResults/startAt should remain so callers can detect comment
	// truncation on very active issues.
	issueLine, _, _, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	fields := issueLine.JiraIssue.Serialized["fields"].(map[string]any)
	cmt := fields["comment"].(map[string]any)

	if _, ok := cmt["comments"]; ok {
		t.Error("issue line still contains fields.comment.comments[] (should be lifted)")
	}
	for _, k := range []string{"total", "maxResults", "startAt"} {
		if _, ok := cmt[k]; !ok {
			t.Errorf("fields.comment.%s missing (should be preserved)", k)
		}
	}
}

func TestSplitIssueRawPreservesUnknownFields(t *testing.T) {
	// pkg/jira.Issue does not model `customfield_*` etc. Those fields must
	// round-trip through Serialized unchanged.
	issueLine, _, _, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	if issueLine.JiraIssue.Serialized["customfield_10016"] != float64(5) {
		t.Errorf("customfield_10016 = %v", issueLine.JiraIssue.Serialized["customfield_10016"])
	}
	if issueLine.JiraIssue.Serialized["id"] != "10042" {
		t.Errorf("id = %v (must be present in Serialized for ID() to find it)", issueLine.JiraIssue.Serialized["id"])
	}
}

func TestSplitIssueRawInjectsIssueKey(t *testing.T) {
	// The critical test: every comment must carry the parent issueKey,
	// both in Runtime (for typed access) and Serialized (so it survives
	// to disk and grep).
	_, commentLines, _, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	for i, cl := range commentLines {
		if cl.JiraComment.Runtime.IssueKey != "ENG-142" {
			t.Errorf("comment[%d] Runtime.IssueKey = %q, want ENG-142", i, cl.JiraComment.Runtime.IssueKey)
		}
		if cl.JiraComment.Serialized["issueKey"] != "ENG-142" {
			t.Errorf("comment[%d] Serialized[issueKey] = %v, want ENG-142", i, cl.JiraComment.Serialized["issueKey"])
		}

		// And confirm it survives marshal: the literal bytes must contain
		// the field so `rg '"issueKey":"ENG-142"'` works against the
		// JSONL files on disk.
		data, err := modelv1.Marshal(cl)
		if err != nil {
			t.Fatalf("Marshal comment[%d]: %v", i, err)
		}
		if !strings.Contains(string(data), `"issueKey":"ENG-142"`) {
			t.Errorf("marshaled comment[%d] missing literal issueKey: %s", i, data)
		}
	}
}

func TestSplitIssueRawCommentRuntimeFields(t *testing.T) {
	_, commentLines, _, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	c0 := commentLines[0].JiraComment.Runtime
	if c0.ID != "10501" {
		t.Errorf("comment[0].ID = %q", c0.ID)
	}
	if c0.Created != "2026-04-08T14:04:31.883+0000" {
		t.Errorf("comment[0].Created = %q", c0.Created)
	}
	if c0.Updated != "2026-04-08T14:04:31.883+0000" {
		t.Errorf("comment[0].Updated = %q", c0.Updated)
	}
	// jsdPublic, body, author, self live in Serialized only.
	c0s := commentLines[0].JiraComment.Serialized
	if c0s["jsdPublic"] != true {
		t.Errorf("Serialized[jsdPublic] = %v", c0s["jsdPublic"])
	}
	if c0s["body"] != "Looks good." {
		t.Errorf("Serialized[body] = %v", c0s["body"])
	}
}

func TestSplitIssueRawNoComments(t *testing.T) {
	body := `{"id":"10042","key":"ENG-142","fields":{"updated":"2026-04-05T09:44:15.076+0000","comment":{"total":0,"maxResults":1000,"startAt":0,"comments":[]}}}`
	issueLine, commentLines, _, err := splitIssueRaw("ENG-142", []byte(body))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	if len(commentLines) != 0 {
		t.Errorf("got %d comment lines, want 0", len(commentLines))
	}
	// Issue line still has comment metadata.
	cmt := issueLine.JiraIssue.Serialized["fields"].(map[string]any)["comment"].(map[string]any)
	if cmt["total"] != float64(0) {
		t.Errorf("comment.total = %v", cmt["total"])
	}
}

func TestSplitIssueRawNoCommentObject(t *testing.T) {
	// Some endpoints (or `expand` settings) omit fields.comment entirely.
	// Splitter should return zero comment lines without erroring.
	body := `{"id":"10042","key":"ENG-142","fields":{"updated":"2026-04-05T09:44:15.076+0000"}}`
	issueLine, commentLines, _, err := splitIssueRaw("ENG-142", []byte(body))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}
	if len(commentLines) != 0 {
		t.Errorf("got %d comment lines, want 0", len(commentLines))
	}
	if issueLine.Type != modelv1.LineJiraIssue {
		t.Error("issue line type wrong")
	}
}

func TestSplitIssueRawMalformed(t *testing.T) {
	if _, _, _, err := splitIssueRaw("ENG-1", []byte("not json")); err == nil {
		t.Error("expected error on malformed body, got nil")
	}
}

// TestLiftCommentsWrongType exercises liftComments directly against
// schema-drift inputs that the strict pkg/jira.Issue unmarshal would
// reject upstream. The defensive type checks inside liftComments are
// the second line of defence: if the strict parse is ever loosened
// (or pkg/jira changes how it models fields.comment), we still want
// type mismatches to log + drop rather than crash + corrupt.
func TestLiftCommentsWrongType(t *testing.T) {
	cases := []struct {
		name      string
		input     map[string]any
		wantCount int
	}{
		{
			"fields absent",
			map[string]any{"id": "1", "key": "ENG-1"},
			0,
		},
		{
			"fields.comment absent",
			map[string]any{"fields": map[string]any{"updated": "x"}},
			0,
		},
		{
			"fields.comment.comments absent",
			map[string]any{"fields": map[string]any{"comment": map[string]any{"total": 0}}},
			0,
		},
		{
			"fields.comment is a string",
			map[string]any{"fields": map[string]any{"comment": "surprise"}},
			0,
		},
		{
			"fields.comment.comments is a string",
			map[string]any{"fields": map[string]any{"comment": map[string]any{"comments": "oops"}}},
			0,
		},
		{
			"individual comment is a string",
			map[string]any{"fields": map[string]any{"comment": map[string]any{
				"comments": []any{
					"oops",
					map[string]any{"id": "c2"},
				},
			}}},
			1, // only the well-formed map survives
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := liftComments("ENG-1", c.input)
			if len(got) != c.wantCount {
				t.Errorf("liftComments returned %d, want %d", len(got), c.wantCount)
			}
		})
	}
}

func TestSplitIssueRawIssueLineMarshalSurvivesRoundTrip(t *testing.T) {
	// End-to-end: run splitIssueRaw, marshal each line, parse it back.
	// Catches any Type/Serialized routing bugs that wouldn't show up in
	// the per-field assertions above.
	issueLine, commentLines, _, err := splitIssueRaw("ENG-142", []byte(realIssueBody))
	if err != nil {
		t.Fatalf("splitIssueRaw: %v", err)
	}

	// Issue.
	data, err := modelv1.Marshal(issueLine)
	if err != nil {
		t.Fatalf("Marshal issue: %v", err)
	}
	parsed, err := modelv1.Parse(string(data))
	if err != nil {
		t.Fatalf("Parse issue: %v", err)
	}
	if parsed.Type != modelv1.LineJiraIssue {
		t.Errorf("round-tripped issue Type = %q", parsed.Type)
	}
	// Confirm dedup-id is recoverable post-roundtrip.
	id, ok := parsed.ID()
	if !ok || id != "10042" {
		t.Errorf("ID() = (%q, %v) on parsed issue", id, ok)
	}

	// First comment.
	data, err = modelv1.Marshal(commentLines[0])
	if err != nil {
		t.Fatalf("Marshal comment: %v", err)
	}
	parsed, err = modelv1.Parse(string(data))
	if err != nil {
		t.Fatalf("Parse comment: %v", err)
	}
	if parsed.JiraComment.Runtime.IssueKey != "ENG-142" {
		t.Errorf("round-tripped comment IssueKey = %q", parsed.JiraComment.Runtime.IssueKey)
	}

	// Sanity: pretty-print byte length, not for assertion.
	_ = json.RawMessage(data)
}
