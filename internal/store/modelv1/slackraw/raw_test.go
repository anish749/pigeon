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

