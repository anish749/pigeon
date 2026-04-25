package modelv1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestMarshalParseJiraIssue(t *testing.T) {
	// Approximation of what GetIssueRaw returns from Jira Cloud — note the
	// numeric "+0000" offset (not "Z"), and id-as-string (Jira returns it
	// quoted even though it's numeric).
	serialized := map[string]any{
		"id":   "10042",
		"key":  "ENG-142",
		"self": "https://acme.atlassian.net/rest/api/3/issue/10042",
		"fields": map[string]any{
			"summary":  "Fix login timeout",
			"updated":  "2026-04-05T09:44:15.076+0000",
			"created":  "2026-04-02T15:14:52.509+0000",
			"status":   map[string]any{"name": "In Progress"},
			"priority": map[string]any{"name": "High"},
		},
	}

	orig := Line{
		Type: LineJiraIssue,
		JiraIssue: &JiraIssue{
			Runtime: jira.Issue{
				Key: "ENG-142",
				Fields: jira.IssueFields{
					Summary: "Fix login timeout",
					Updated: "2026-04-05T09:44:15.076+0000",
					Created: "2026-04-02T15:14:52.509+0000",
				},
			},
			Serialized: serialized,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Type discriminator injected.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if raw["type"] != "jira-issue" {
		t.Errorf("type = %q, want %q", raw["type"], "jira-issue")
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != LineJiraIssue {
		t.Errorf("Type = %q, want %q", got.Type, LineJiraIssue)
	}
	if got.JiraIssue == nil {
		t.Fatal("JiraIssue is nil")
	}
	if got.JiraIssue.Runtime.Key != "ENG-142" {
		t.Errorf("Runtime.Key = %q", got.JiraIssue.Runtime.Key)
	}
	if got.JiraIssue.Runtime.Fields.Updated != "2026-04-05T09:44:15.076+0000" {
		t.Errorf("Runtime.Fields.Updated = %q", got.JiraIssue.Runtime.Fields.Updated)
	}
	// Serialized preserves all original fields.
	if got.JiraIssue.Serialized["self"] != "https://acme.atlassian.net/rest/api/3/issue/10042" {
		t.Errorf("Serialized[self] = %v", got.JiraIssue.Serialized["self"])
	}
	// "type" stripped from Serialized.
	if _, ok := got.JiraIssue.Serialized["type"]; ok {
		t.Error("Serialized must not contain 'type'")
	}
}

func TestJiraIssueID(t *testing.T) {
	// The dedup key for Jira issues is the numeric `id` from the raw HTTP
	// body. pkg/jira.Issue doesn't model `id`, so ID() must read it from
	// Serialized.
	l := Line{
		Type: LineJiraIssue,
		JiraIssue: &JiraIssue{
			Runtime:    jira.Issue{Key: "ENG-142"},
			Serialized: map[string]any{"id": "10042"},
		},
	}
	id, ok := l.ID()
	if !ok {
		t.Fatal("ID() returned false")
	}
	if id != "10042" {
		t.Errorf("ID() = %q, want %q", id, "10042")
	}
}

func TestJiraIssueIDMissing(t *testing.T) {
	// If the raw body has no `id` field, ID() returns false. This should
	// never happen in practice but we don't want to panic on garbage input.
	l := Line{
		Type: LineJiraIssue,
		JiraIssue: &JiraIssue{
			Runtime:    jira.Issue{Key: "ENG-142"},
			Serialized: map[string]any{},
		},
	}
	if _, ok := l.ID(); ok {
		t.Error("ID() returned ok for missing id field")
	}
}

// TestJiraCommentInjectsIssueKey is the critical test that protects against
// the Linear comment shape's bug. A jira-comment line, in isolation, MUST
// self-identify its parent issue. This test asserts that issueKey survives
// the marshal/parse round-trip, both as a Runtime field and as a top-level
// JSON key (so `rg '"issueKey":"ENG-142"'` matches).
func TestJiraCommentInjectsIssueKey(t *testing.T) {
	serialized := map[string]any{
		"id":        "10501",
		"issueKey":  "ENG-142",
		"self":      "https://acme.atlassian.net/rest/api/3/issue/10042/comment/10501",
		"author":    map[string]any{"displayName": "Bob"},
		"body":      "Looks good.",
		"created":   "2026-04-08T14:04:31.883+0000",
		"updated":   "2026-04-08T14:04:31.883+0000",
		"jsdPublic": true,
	}

	orig := Line{
		Type: LineJiraComment,
		JiraComment: &JiraComment{
			Runtime: JiraCommentRuntime{
				ID:       "10501",
				IssueKey: "ENG-142",
				Created:  "2026-04-08T14:04:31.883+0000",
				Updated:  "2026-04-08T14:04:31.883+0000",
			},
			Serialized: serialized,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// 1. The marshaled bytes must contain "issueKey":"ENG-142" verbatim, so
	//    grep across all jira files can filter "comments on ENG-142" without
	//    decoding JSON.
	if !strings.Contains(string(data), `"issueKey":"ENG-142"`) {
		t.Errorf("marshaled comment missing literal `\"issueKey\":\"ENG-142\"`: %s", data)
	}

	// 2. Round-trip preserves the field on both sides.
	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.JiraComment == nil {
		t.Fatal("JiraComment is nil")
	}
	if got.JiraComment.Runtime.IssueKey != "ENG-142" {
		t.Errorf("Runtime.IssueKey = %q, want %q", got.JiraComment.Runtime.IssueKey, "ENG-142")
	}
	if got.JiraComment.Serialized["issueKey"] != "ENG-142" {
		t.Errorf("Serialized[issueKey] = %v, want ENG-142", got.JiraComment.Serialized["issueKey"])
	}
}

func TestJiraCommentID(t *testing.T) {
	l := Line{
		Type: LineJiraComment,
		JiraComment: &JiraComment{
			Runtime: JiraCommentRuntime{ID: "10501", IssueKey: "ENG-142"},
		},
	}
	id, ok := l.ID()
	if !ok {
		t.Fatal("ID() returned false")
	}
	if id != "10501" {
		t.Errorf("ID() = %q, want %q", id, "10501")
	}
}

// TestJiraTsParsesNumericOffset verifies Ts() handles Jira's non-canonical
// "+0000" offset (no colon). Jira's REST API returns this format on every
// timestamp; if Ts() only knew time.RFC3339 ("Z" or "+00:00"), every Jira
// line would silently report a zero timestamp.
func TestJiraTsParsesNumericOffset(t *testing.T) {
	l := Line{
		Type: LineJiraIssue,
		JiraIssue: &JiraIssue{
			Runtime: jira.Issue{
				Fields: jira.IssueFields{Updated: "2026-04-05T09:44:15.076+0000"},
			},
		},
	}
	ts := l.Ts()
	if ts.IsZero() {
		t.Fatal("Ts() returned zero for valid Jira timestamp")
	}
	want := time.Date(2026, 4, 5, 9, 44, 15, 76000000, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("Ts() = %v, want %v", ts, want)
	}
}

func TestJiraTsParsesZForm(t *testing.T) {
	// Some Cloud responses use canonical "Z". Both forms should parse.
	l := Line{
		Type: LineJiraComment,
		JiraComment: &JiraComment{
			Runtime: JiraCommentRuntime{Created: "2026-04-08T14:04:31Z"},
		},
	}
	ts := l.Ts()
	if ts.IsZero() {
		t.Fatal("Ts() returned zero for Z-form timestamp")
	}
	want := time.Date(2026, 4, 8, 14, 4, 31, 0, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("Ts() = %v, want %v", ts, want)
	}
}

func TestJiraRoundTripPreservesUnknownFields(t *testing.T) {
	// Real Jira responses carry many fields pkg/jira.Issue does not model
	// (customfield_*, attachment[], worklog, etc.). They must round-trip
	// unchanged via the Serialized map.
	raw := `{"type":"jira-issue","id":"10042","key":"ENG-142","customfield_10016":42,"fields":{"updated":"2026-04-05T09:44:15.076+0000","attachment":[{"id":"99","filename":"log.txt"}]}}`

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := Marshal(parsed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["customfield_10016"] != float64(42) {
		t.Errorf("customfield_10016 = %v, want 42", out["customfield_10016"])
	}
	fields, ok := out["fields"].(map[string]any)
	if !ok {
		t.Fatal("fields missing or not an object")
	}
	att, ok := fields["attachment"].([]any)
	if !ok || len(att) != 1 {
		t.Fatalf("attachment[] not preserved: %v", fields["attachment"])
	}
}
