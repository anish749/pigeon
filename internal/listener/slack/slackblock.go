package slack

import (
	"encoding/json"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// buildSlackBlockLine creates a SlackBlock line from a Slack message whose
// Text is empty but whose blocks/attachments carry structured content.
// The blocks and attachments are serialized as raw JSON in a map alongside
// the message metadata (id, ts, sender, etc.).
func buildSlackBlockLine(slackTS string, ts time.Time, sender, senderID string, via modelv1.Via, isReply bool, blocks goslack.Blocks, attachments []goslack.Attachment) modelv1.Line {
	runtime := modelv1.SlackBlockRuntime{
		ID:       slackTS,
		Ts:       ts.Format(time.RFC3339),
		Sender:   sender,
		SenderID: senderID,
		Via:      via,
		Reply:    isReply,
	}

	// Build the serialized map with the raw block data alongside metadata.
	serialized := map[string]any{
		"id":     slackTS,
		"ts":     ts.Format(time.RFC3339),
		"sender": sender,
		"from":   senderID,
	}
	if via != "" {
		serialized["via"] = string(via)
	}
	if isReply {
		serialized["reply"] = true
	}

	// Serialize blocks and attachments as raw JSON values to preserve
	// the exact Slack API structure without loss.
	if len(blocks.BlockSet) > 0 {
		serialized["blocks"] = marshalToAny(blocks)
	}
	if len(attachments) > 0 {
		serialized["attachments"] = marshalToAny(attachments)
	}

	return modelv1.Line{
		Type:       modelv1.LineSlackBlock,
		SlackBlock: &modelv1.SlackBlock{Runtime: runtime, Serialized: serialized},
	}
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
