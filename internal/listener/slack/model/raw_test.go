package model

import (
	"encoding/json"
	"testing"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestExtractRaw_Empty(t *testing.T) {
	raw := ExtractRaw(goslack.Msg{})
	if raw != nil {
		t.Errorf("ExtractRaw(empty msg) = %v, want nil", raw)
	}
}

func TestExtractRaw_Files(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "plan.md", Mimetype: "text/plain", Size: 3636},
		},
	}
	raw := ExtractRaw(msg)
	if raw == nil {
		t.Fatal("ExtractRaw returned nil, want non-nil")
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

func TestExtractRaw_Blocks(t *testing.T) {
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
	raw := ExtractRaw(msg)
	if raw == nil {
		t.Fatal("ExtractRaw returned nil, want non-nil")
	}
	if _, ok := raw["blocks"]; !ok {
		t.Error("raw[blocks] missing")
	}
}

func TestExtractRaw_RoundTrip(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "image.png", Mimetype: "image/png", Size: 12345},
		},
		Attachments: []goslack.Attachment{
			{Title: "PR #42", Text: "Fix the bug", Fallback: "PR #42 - Fix the bug"},
		},
	}
	raw := ExtractRaw(msg)
	if raw == nil {
		t.Fatal("ExtractRaw returned nil")
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

func TestExtractRaw_MsgLineRoundTrip(t *testing.T) {
	// Test that Raw on MsgLine survives marshal → unmarshal via Line.
	raw := map[string]any{
		"files": []any{
			map[string]any{"name": "doc.pdf", "size": float64(999)},
		},
	}
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:     "123",
			Sender: "Alice",
			Text:   "",
			Raw:    raw,
		},
	}

	data, err := modelv1.Marshal(line)
	if err != nil {
		t.Fatalf("marshal line: %v", err)
	}

	parsed, err := modelv1.Parse(string(data))
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if parsed.Msg == nil {
		t.Fatal("parsed.Msg is nil")
	}
	if parsed.Msg.Raw == nil {
		t.Fatal("parsed.Msg.Raw is nil after round-trip")
	}
	files, ok := parsed.Msg.Raw["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("round-tripped files = %v, want slice of 1", parsed.Msg.Raw["files"])
	}
	if files[0].(map[string]any)["name"] != "doc.pdf" {
		t.Errorf("file name = %v, want doc.pdf", files[0].(map[string]any)["name"])
	}
}
