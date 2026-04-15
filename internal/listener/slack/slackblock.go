package slack

import (
	"encoding/json"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// slackBlockPayload builds the serialized map for a Slack block message.
// The caller passes this to modelv1.NewSlackBlockLine.
func slackBlockPayload(slackTS string, ts time.Time, sender, senderID string, via modelv1.Via, isReply bool, blocks goslack.Blocks, attachments []goslack.Attachment) map[string]any {
	m := map[string]any{
		"id":     slackTS,
		"ts":     ts,
		"sender": sender,
		"from":   senderID,
	}
	if via != "" {
		m["via"] = string(via)
	}
	if isReply {
		m["reply"] = true
	}
	if len(blocks.BlockSet) > 0 {
		m["blocks"] = marshalToAny(blocks)
	}
	if len(attachments) > 0 {
		m["attachments"] = marshalToAny(attachments)
	}
	return m
}

// marshalToAny round-trips a value through JSON to get a map[string]any /
// []any representation suitable for inclusion in a serialized map.
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
