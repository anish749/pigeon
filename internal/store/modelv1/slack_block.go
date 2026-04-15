package modelv1

import (
	"encoding/json"
	"fmt"
	"time"
)

// SlackBlock holds two representations of a Slack message whose content
// lives in Block Kit blocks or legacy attachments (i.e. Text is empty).
//
// Runtime contains the fields pigeon needs for dedup (ID), cursor
// tracking (Ts), and display (Sender, SenderID). Serialized is a
// JSON-shaped map that Marshal writes verbatim to disk, preserving the
// raw blocks/attachments exactly as the Slack API returned them.
//
// Only Serialized is persisted. Treat Runtime as a read-only view.
type SlackBlock struct {
	Runtime    SlackBlockRuntime
	Serialized map[string]any
}

// SlackBlockRuntime holds the fields pigeon needs for dedup, timestamp
// extraction, and display. Everything else lives in Serialized.
type SlackBlockRuntime struct {
	ID       string    `json:"id"`       // Slack message timestamp (e.g. "1711568938.123456")
	Ts       time.Time `json:"ts"`       // timestamp for storage ordering
	Sender   string    `json:"sender"`   // display name
	SenderID string    `json:"from"`     // platform user ID
	Via      Via       `json:"via,omitempty"`
	Reply    bool      `json:"reply,omitempty"`
}

// NewSlackBlockLine creates a SlackBlock line from a serialized map containing
// the message metadata and raw block/attachment data. Runtime is derived from
// the map via JSON round-trip, following the same pattern as Linear and GWS.
func NewSlackBlockLine(serialized map[string]any) (Line, error) {
	raw, err := json.Marshal(serialized)
	if err != nil {
		return Line{}, fmt.Errorf("marshal slack block: %w", err)
	}
	var runtime SlackBlockRuntime
	if err := json.Unmarshal(raw, &runtime); err != nil {
		return Line{}, fmt.Errorf("parse slack block runtime: %w", err)
	}
	return Line{
		Type:       LineSlackBlock,
		SlackBlock: &SlackBlock{Runtime: runtime, Serialized: serialized},
	}, nil
}

// NewMsgLine creates a message Line with the given fields.
func NewMsgLine(id string, ts time.Time, sender, senderID, text string, via Via, reply bool) Line {
	return Line{
		Type: LineMessage,
		Msg: &MsgLine{
			ID:       id,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Via:      via,
			Text:     text,
			Reply:    reply,
		},
	}
}

