package modelv1

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMarshalParseSlackBlock(t *testing.T) {
	serialized := map[string]any{
		"id":     "1711568938.123456",
		"ts":     "2026-03-27T18:15:38Z",
		"sender": "DeployBot",
		"from":   "B04DEPLOY",
		"blocks": []any{
			map[string]any{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": "Deployment complete",
				},
			},
		},
		"attachments": []any{
			map[string]any{
				"text":     "build #42 passed",
				"fallback": "build passed",
			},
		},
	}

	orig := Line{
		Type: LineSlackBlock,
		SlackBlock: &SlackBlock{
			Runtime: SlackBlockRuntime{
				ID:       "1711568938.123456",
				Ts:       "2026-03-27T18:15:38Z",
				Sender:   "DeployBot",
				SenderID: "B04DEPLOY",
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
	if raw["type"] != "slack-block" {
		t.Errorf("type = %q, want %q", raw["type"], "slack-block")
	}
	if raw["id"] != "1711568938.123456" {
		t.Errorf("id = %v, want 1711568938.123456", raw["id"])
	}

	// Round-trip through Parse.
	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != LineSlackBlock {
		t.Errorf("Type = %q, want %q", got.Type, LineSlackBlock)
	}
	if got.SlackBlock == nil {
		t.Fatal("SlackBlock is nil")
	}
	if got.SlackBlock.Runtime.ID != "1711568938.123456" {
		t.Errorf("Runtime.ID = %q", got.SlackBlock.Runtime.ID)
	}
	if got.SlackBlock.Runtime.Sender != "DeployBot" {
		t.Errorf("Runtime.Sender = %q", got.SlackBlock.Runtime.Sender)
	}
	if got.SlackBlock.Runtime.SenderID != "B04DEPLOY" {
		t.Errorf("Runtime.SenderID = %q", got.SlackBlock.Runtime.SenderID)
	}

	// Serialized should preserve blocks and attachments (minus "type").
	blocks, ok := got.SlackBlock.Serialized["blocks"]
	if !ok {
		t.Fatal("Serialized missing 'blocks' key")
	}
	blockSlice, ok := blocks.([]any)
	if !ok || len(blockSlice) != 1 {
		t.Errorf("blocks = %v, want 1-element slice", blocks)
	}
	if _, ok := got.SlackBlock.Serialized["type"]; ok {
		t.Error("Serialized should not contain 'type' key")
	}
}

func TestSlackBlockID(t *testing.T) {
	l := Line{
		Type: LineSlackBlock,
		SlackBlock: &SlackBlock{
			Runtime: SlackBlockRuntime{ID: "1711568938.123456"},
		},
	}
	id, ok := l.ID()
	if !ok {
		t.Fatal("ID() returned false")
	}
	if id != "1711568938.123456" {
		t.Errorf("ID() = %q, want %q", id, "1711568938.123456")
	}
}

func TestSlackBlockTs(t *testing.T) {
	l := Line{
		Type: LineSlackBlock,
		SlackBlock: &SlackBlock{
			Runtime: SlackBlockRuntime{Ts: "2026-03-27T18:15:38Z"},
		},
	}
	ts := l.Ts()
	if ts.IsZero() {
		t.Fatal("Ts() returned zero time")
	}
	want := time.Date(2026, 3, 27, 18, 15, 38, 0, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("Ts() = %v, want %v", ts, want)
	}
}

func TestSlackBlockTsInvalid(t *testing.T) {
	l := Line{
		Type: LineSlackBlock,
		SlackBlock: &SlackBlock{
			Runtime: SlackBlockRuntime{Ts: "not-a-date"},
		},
	}
	ts := l.Ts()
	if !ts.IsZero() {
		t.Errorf("Ts() = %v, want zero time for invalid date", ts)
	}
}

func TestSlackBlockRoundTripPreservesUnknownFields(t *testing.T) {
	raw := `{"type":"slack-block","id":"ts1","ts":"2026-01-01T00:00:00Z","sender":"Bot","from":"B1","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"hello"}}],"customField":"preserve-me"}`

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
}

func TestSlackBlockWithViaAndReply(t *testing.T) {
	serialized := map[string]any{
		"id":     "ts1",
		"ts":     "2026-03-27T18:15:38Z",
		"sender": "sent to pigeon by Alice",
		"from":   "U123",
		"via":    "to-pigeon",
		"reply":  true,
		"blocks": []any{
			map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "hello"}},
		},
	}

	orig := Line{
		Type: LineSlackBlock,
		SlackBlock: &SlackBlock{
			Runtime: SlackBlockRuntime{
				ID:       "ts1",
				Ts:       "2026-03-27T18:15:38Z",
				Sender:   "sent to pigeon by Alice",
				SenderID: "U123",
				Via:      ViaToPigeon,
				Reply:    true,
			},
			Serialized: serialized,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.SlackBlock.Runtime.Via != ViaToPigeon {
		t.Errorf("Via = %q, want %q", got.SlackBlock.Runtime.Via, ViaToPigeon)
	}
	if !got.SlackBlock.Runtime.Reply {
		t.Error("Reply = false, want true")
	}
}
