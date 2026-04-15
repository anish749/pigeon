package slack

import (
	"encoding/json"
	"fmt"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// slackBlockLine marshals a Slack message into a SlackBlock line. The Slack
// Msg struct is serialized to JSON, then enriched with pigeon-specific fields
// (resolved sender name, via, reply) before being passed to the model layer.
// This follows the same pattern as Linear and Drive: the raw API data is the
// serialized map.
func slackBlockLine(msg goslack.Msg, ts time.Time, sender string, via modelv1.Via, isReply bool) (modelv1.Line, error) {
	raw, err := json.Marshal(msg)
	if err != nil {
		return modelv1.Line{}, fmt.Errorf("marshal slack message: %w", err)
	}
	var serialized map[string]any
	if err := json.Unmarshal(raw, &serialized); err != nil {
		return modelv1.Line{}, fmt.Errorf("unmarshal slack message: %w", err)
	}

	// Enrich with pigeon-specific fields not present in the Slack struct.
	serialized["id"] = msg.Timestamp
	serialized["ts"] = ts
	serialized["sender"] = sender
	serialized["from"] = msg.User
	if msg.User == "" {
		serialized["from"] = msg.BotID
	}
	if via != "" {
		serialized["via"] = string(via)
	}
	if isReply {
		serialized["reply"] = true
	}

	return modelv1.NewSlackBlockLine(serialized)
}
