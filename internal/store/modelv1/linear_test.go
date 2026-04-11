package modelv1

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMarshalParseLinearIssue(t *testing.T) {
	serialized := map[string]any{
		"id":            "c610f566-fc1d-40db-b129-8070743f9559",
		"identifier":    "ENG-101",
		"title":         "Fix login bug",
		"updatedAt":     "2026-04-05T09:44:15.076Z",
		"state":         map[string]any{"name": "In Progress", "type": "started"},
		"priority":      float64(2),
		"priorityLabel": "High",
	}

	orig := Line{
		Type: LineLinearIssue,
		Issue: &LinearIssue{
			Runtime: LinearIssueRuntime{
				ID:         "c610f566-fc1d-40db-b129-8070743f9559",
				Identifier: "ENG-101",
				UpdatedAt:  "2026-04-05T09:44:15.076Z",
			},
			Serialized: serialized,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify the type discriminator is injected.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if raw["type"] != "linear-issue" {
		t.Errorf("type = %q, want %q", raw["type"], "linear-issue")
	}
	if raw["id"] != "c610f566-fc1d-40db-b129-8070743f9559" {
		t.Errorf("id = %v, want c610f566-...", raw["id"])
	}

	// Round-trip through Parse.
	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != LineLinearIssue {
		t.Errorf("Type = %q, want %q", got.Type, LineLinearIssue)
	}
	if got.Issue == nil {
		t.Fatal("Issue is nil")
	}
	if got.Issue.Runtime.ID != "c610f566-fc1d-40db-b129-8070743f9559" {
		t.Errorf("Runtime.ID = %q", got.Issue.Runtime.ID)
	}
	if got.Issue.Runtime.Identifier != "ENG-101" {
		t.Errorf("Runtime.Identifier = %q", got.Issue.Runtime.Identifier)
	}
	if got.Issue.Runtime.UpdatedAt != "2026-04-05T09:44:15.076Z" {
		t.Errorf("Runtime.UpdatedAt = %q", got.Issue.Runtime.UpdatedAt)
	}

	// Serialized should preserve all original fields (minus "type").
	if got.Issue.Serialized["title"] != "Fix login bug" {
		t.Errorf("Serialized[title] = %v", got.Issue.Serialized["title"])
	}
	if got.Issue.Serialized["priorityLabel"] != "High" {
		t.Errorf("Serialized[priorityLabel] = %v", got.Issue.Serialized["priorityLabel"])
	}
	// "type" is stripped from Serialized (it's the storage discriminator).
	if _, ok := got.Issue.Serialized["type"]; ok {
		t.Error("Serialized should not contain 'type' key")
	}
}

func TestMarshalParseLinearComment(t *testing.T) {
	serialized := map[string]any{
		"id":        "0bb50b07-3f72-4412-ad63-e6aca4dd5dea",
		"body":      "Looks good to me!",
		"createdAt": "2026-04-08T14:04:31.883Z",
		"url":       "https://linear.app/team/issue/ENG-101#comment-0bb50b07",
		"user":      map[string]any{"name": "Alice", "displayName": "alice"},
		"parent":    nil,
	}

	orig := Line{
		Type: LineLinearComment,
		LinearComment: &LinearComment{
			Runtime: LinearCommentRuntime{
				ID:        "0bb50b07-3f72-4412-ad63-e6aca4dd5dea",
				CreatedAt: "2026-04-08T14:04:31.883Z",
			},
			Serialized: serialized,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	if raw["type"] != "linear-comment" {
		t.Errorf("type = %q, want %q", raw["type"], "linear-comment")
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != LineLinearComment {
		t.Errorf("Type = %q, want %q", got.Type, LineLinearComment)
	}
	if got.LinearComment == nil {
		t.Fatal("LinearComment is nil")
	}
	if got.LinearComment.Runtime.ID != "0bb50b07-3f72-4412-ad63-e6aca4dd5dea" {
		t.Errorf("Runtime.ID = %q", got.LinearComment.Runtime.ID)
	}
	if got.LinearComment.Runtime.CreatedAt != "2026-04-08T14:04:31.883Z" {
		t.Errorf("Runtime.CreatedAt = %q", got.LinearComment.Runtime.CreatedAt)
	}
	if got.LinearComment.Serialized["body"] != "Looks good to me!" {
		t.Errorf("Serialized[body] = %v", got.LinearComment.Serialized["body"])
	}
	if _, ok := got.LinearComment.Serialized["type"]; ok {
		t.Error("Serialized should not contain 'type' key")
	}
}

func TestLinearIssueID(t *testing.T) {
	l := Line{
		Type: LineLinearIssue,
		Issue: &LinearIssue{
			Runtime: LinearIssueRuntime{ID: "abc-123"},
		},
	}
	id, ok := l.ID()
	if !ok {
		t.Fatal("ID() returned false")
	}
	if id != "abc-123" {
		t.Errorf("ID() = %q, want %q", id, "abc-123")
	}
}

func TestLinearCommentID(t *testing.T) {
	l := Line{
		Type: LineLinearComment,
		LinearComment: &LinearComment{
			Runtime: LinearCommentRuntime{ID: "def-456"},
		},
	}
	id, ok := l.ID()
	if !ok {
		t.Fatal("ID() returned false")
	}
	if id != "def-456" {
		t.Errorf("ID() = %q, want %q", id, "def-456")
	}
}

func TestLinearIssueTs(t *testing.T) {
	l := Line{
		Type: LineLinearIssue,
		Issue: &LinearIssue{
			Runtime: LinearIssueRuntime{UpdatedAt: "2026-04-05T09:44:15.076Z"},
		},
	}
	ts := l.Ts()
	if ts.IsZero() {
		t.Fatal("Ts() returned zero time")
	}
	want := time.Date(2026, 4, 5, 9, 44, 15, 76000000, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("Ts() = %v, want %v", ts, want)
	}
}

func TestLinearCommentTs(t *testing.T) {
	l := Line{
		Type: LineLinearComment,
		LinearComment: &LinearComment{
			Runtime: LinearCommentRuntime{CreatedAt: "2026-04-08T14:04:31.883Z"},
		},
	}
	ts := l.Ts()
	if ts.IsZero() {
		t.Fatal("Ts() returned zero time")
	}
	want := time.Date(2026, 4, 8, 14, 4, 31, 883000000, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("Ts() = %v, want %v", ts, want)
	}
}

func TestLinearIssueTsInvalid(t *testing.T) {
	l := Line{
		Type: LineLinearIssue,
		Issue: &LinearIssue{
			Runtime: LinearIssueRuntime{UpdatedAt: "not-a-date"},
		},
	}
	ts := l.Ts()
	if !ts.IsZero() {
		t.Errorf("Ts() = %v, want zero time for invalid date", ts)
	}
}

func TestLinearRoundTripPreservesUnknownFields(t *testing.T) {
	// Simulate a CLI response with fields the Runtime struct doesn't know about.
	raw := `{"type":"linear-issue","id":"abc","identifier":"ENG-1","updatedAt":"2026-01-01T00:00:00Z","customField":"preserve-me","nested":{"deep":true}}`

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Marshal back and verify unknown fields survived.
	data, err := Marshal(parsed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["customField"] != "preserve-me" {
		t.Errorf("customField = %v, want preserve-me", out["customField"])
	}
	nested, ok := out["nested"].(map[string]any)
	if !ok || nested["deep"] != true {
		t.Errorf("nested = %v, want {deep: true}", out["nested"])
	}
}
