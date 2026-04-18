package slackraw

import (
	"encoding/json"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestNewSlackRawContent_Empty(t *testing.T) {
	rc := NewSlackRawContent(goslack.Msg{})
	raw := rc.AsSerializable()
	if raw != nil {
		t.Errorf("empty msg raw = %v, want nil", raw)
	}
}

func TestNewSlackRawContent_Files(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "plan.md", Mimetype: "text/plain", Size: 3636},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil, want non-nil")
	}
	files, ok := raw["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("raw[files] = %v, want slice of 1", raw["files"])
	}
	file := files[0].(map[string]any)
	if file["name"] != "plan.md" {
		t.Errorf("file name = %v, want plan.md", file["name"])
	}
}

func TestNewSlackRawContent_Blocks(t *testing.T) {
	msg := goslack.Msg{
		Blocks: goslack.Blocks{
			BlockSet: []goslack.Block{
				goslack.NewRichTextBlock("blk1",
					goslack.NewRichTextSection(
						goslack.NewRichTextSectionTextElement("A huddle started", nil),
					),
				),
			},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil, want non-nil")
	}
	if _, ok := raw["blocks"]; !ok {
		t.Error("raw[blocks] missing")
	}
}

func TestNewSlackRawContent_RoundTrip(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "image.png", Mimetype: "image/png", Size: 12345},
		},
		Attachments: []goslack.Attachment{
			{Title: "PR #42", Text: "Fix the bug", Fallback: "PR #42 - Fix the bug"},
		},
	}
	rc := NewSlackRawContent(msg)
	raw := rc.AsSerializable()
	if raw == nil {
		t.Fatal("raw is nil")
	}

	// Simulate storage: marshal to JSON and unmarshal back.
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored map[string]any
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify files survived.
	files := restored["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files count = %d, want 1", len(files))
	}
	if files[0].(map[string]any)["name"] != "image.png" {
		t.Errorf("file name = %v, want image.png", files[0].(map[string]any)["name"])
	}

	// Verify attachments survived.
	attachments := restored["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("attachments count = %d, want 1", len(attachments))
	}
	if attachments[0].(map[string]any)["title"] != "PR #42" {
		t.Errorf("attachment title = %v, want PR #42", attachments[0].(map[string]any)["title"])
	}
}

func TestFromSerializable_Files(t *testing.T) {
	raw := map[string]any{
		"files": []any{
			map[string]any{"name": "screenshot.png", "mimetype": "image/png", "size": float64(197770)},
		},
	}
	rc, err := FromSerializable(raw)
	if err != nil {
		t.Fatalf("FromSerializable: %v", err)
	}
	if len(rc.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(rc.Files))
	}
	if rc.Files[0].Name != "screenshot.png" {
		t.Errorf("file name = %q, want screenshot.png", rc.Files[0].Name)
	}
	if rc.Files[0].Mimetype != "image/png" {
		t.Errorf("mimetype = %q, want image/png", rc.Files[0].Mimetype)
	}
	if rc.Files[0].Size != 197770 {
		t.Errorf("size = %d, want 197770", rc.Files[0].Size)
	}
}

func TestFromSerializable_Attachments(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{
				"fallback": "Bug created",
				"title":    "BUG-123",
			},
		},
	}
	rc, err := FromSerializable(raw)
	if err != nil {
		t.Fatalf("FromSerializable: %v", err)
	}
	if len(rc.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(rc.Attachments))
	}
	if rc.Attachments[0].Fallback != "Bug created" {
		t.Errorf("fallback = %q, want Bug created", rc.Attachments[0].Fallback)
	}
	if rc.Attachments[0].Title != "BUG-123" {
		t.Errorf("title = %q, want BUG-123", rc.Attachments[0].Title)
	}
}

func TestFromSerializable_Empty(t *testing.T) {
	rc, err := FromSerializable(map[string]any{})
	if err != nil {
		t.Fatalf("FromSerializable: %v", err)
	}
	if len(rc.Files) != 0 || len(rc.Attachments) != 0 || rc.Blocks != nil {
		t.Errorf("expected empty SlackRawContent, got %+v", rc)
	}
}

// TestSlackRawContent_RoundTripViaSerializable tests the full round-trip:
// SlackRawContent → AsSerializable → FromSerializable → SlackRawContent
func TestSlackRawContent_RoundTripViaSerializable(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "image.png", Mimetype: "image/png", Size: 12345, Permalink: "https://example.slack.com/files/U1/F1/image.png"},
		},
		Attachments: []goslack.Attachment{
			{Title: "PR #42", Text: "Fix the bug", Fallback: "PR #42 - Fix the bug"},
		},
	}
	original := NewSlackRawContent(msg)

	// Serialize to map.
	serialized := original.AsSerializable()
	if serialized == nil {
		t.Fatal("AsSerializable returned nil")
	}

	// Deserialize back.
	restored, err := FromSerializable(serialized)
	if err != nil {
		t.Fatalf("FromSerializable: %v", err)
	}

	// Verify files survived the round-trip.
	if len(restored.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(restored.Files))
	}
	if restored.Files[0].Name != "image.png" {
		t.Errorf("file name = %q, want image.png", restored.Files[0].Name)
	}
	if restored.Files[0].Mimetype != "image/png" {
		t.Errorf("mimetype = %q, want image/png", restored.Files[0].Mimetype)
	}
	if restored.Files[0].Size != 12345 {
		t.Errorf("size = %d, want 12345", restored.Files[0].Size)
	}

	// Verify attachments survived the round-trip.
	if len(restored.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(restored.Attachments))
	}
	if restored.Attachments[0].Title != "PR #42" {
		t.Errorf("title = %q, want PR #42", restored.Attachments[0].Title)
	}
	if restored.Attachments[0].Fallback != "PR #42 - Fix the bug" {
		t.Errorf("fallback = %q, want PR #42 - Fix the bug", restored.Attachments[0].Fallback)
	}
}
