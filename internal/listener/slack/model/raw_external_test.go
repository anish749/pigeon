package model_test

import (
	"testing"

	goslack "github.com/slack-go/slack"

	slackmodel "github.com/anish749/pigeon/internal/listener/slack/model"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// TestSlackRawContent_RoundTripViaJSONL tests the full round-trip through
// JSONL storage: SlackRawContent → AsSerializable → MsgLine → JSONL →
// Parse → MsgLine.Raw → FromSerializable → SlackRawContent
func TestSlackRawContent_RoundTripViaJSONL(t *testing.T) {
	msg := goslack.Msg{
		Files: []goslack.File{
			{Name: "doc.pdf", Mimetype: "application/pdf", Size: 99999},
		},
		Attachments: []goslack.Attachment{
			{Fallback: "deploy notification", Fields: []goslack.AttachmentField{
				{Title: "Status", Value: "success"},
			}},
		},
	}
	original := slackmodel.NewSlackRawContent(msg)

	// Write through MsgLine → JSONL.
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:     "123",
			Sender: "bot",
			Text:   "",
			Raw:    original.AsSerializable(),
		},
	}
	data, err := modelv1.Marshal(line)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Read back from JSONL.
	parsed, err := modelv1.Parse(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Deserialize Raw back to typed struct.
	restored, err := slackmodel.FromSerializable(parsed.Msg.Raw)
	if err != nil {
		t.Fatalf("FromSerializable: %v", err)
	}

	if len(restored.Files) != 1 || restored.Files[0].Name != "doc.pdf" {
		t.Errorf("files round-trip failed: %+v", restored.Files)
	}
	if len(restored.Attachments) != 1 || restored.Attachments[0].Fallback != "deploy notification" {
		t.Errorf("attachments round-trip failed: %+v", restored.Attachments)
	}
	if len(restored.Attachments[0].Fields) != 1 || restored.Attachments[0].Fields[0].Title != "Status" {
		t.Errorf("attachment fields round-trip failed: %+v", restored.Attachments[0].Fields)
	}
}

func TestMsgLineRawRoundTrip(t *testing.T) {
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
