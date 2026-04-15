package slack

import (
	"encoding/json"
	"fmt"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// buildSlackBlockLine creates a SlackBlock line from a Slack message whose
// Text is empty but whose blocks/attachments carry structured content.
// The full payload is serialized as a map[string]any; the Runtime is derived
// from that same map, following the same pattern as Linear and GWS line types.
func buildSlackBlockLine(slackTS string, ts time.Time, sender, senderID string, via modelv1.Via, isReply bool, blocks goslack.Blocks, attachments []goslack.Attachment) (modelv1.Line, error) {
	// Build the canonical map — this is what gets persisted.
	serialized := map[string]any{
		"id":     slackTS,
		"ts":     ts,
		"sender": sender,
		"from":   senderID,
	}
	if via != "" {
		serialized["via"] = string(via)
	}
	if isReply {
		serialized["reply"] = true
	}
	if len(blocks.BlockSet) > 0 {
		serialized["blocks"] = marshalToAny(blocks)
	}
	if len(attachments) > 0 {
		serialized["attachments"] = marshalToAny(attachments)
	}

	// Derive Runtime from the serialized map, same as Linear/GWS.
	raw, err := json.Marshal(serialized)
	if err != nil {
		return modelv1.Line{}, fmt.Errorf("marshal slack block: %w", err)
	}
	var runtime modelv1.SlackBlockRuntime
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return modelv1.Line{}, fmt.Errorf("parse slack block runtime: %w", err)
	}

	return modelv1.Line{
		Type:       modelv1.LineSlackBlock,
		SlackBlock: &modelv1.SlackBlock{Runtime: runtime, Serialized: serialized},
	}, nil
}

// marshalToAny round-trips a value through JSON to get a map[string]any /
// []any representation suitable for inclusion in a Serialized map.
func marshalToAny(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
