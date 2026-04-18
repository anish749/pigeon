package model

import (
	"encoding/json"

	goslack "github.com/slack-go/slack"
)

type SerailiableSlackRaw map[string]any

// Marshal the slack-go structs to JSON, then unmarshal back to map[string]any.
// This decouples the storage format from the slack-go types.
type SlackRawContent struct {
	Blocks      *goslack.Blocks      `json:"blocks,omitempty"`
	Attachments []goslack.Attachment `json:"attachments,omitempty"`
	Files       []goslack.File       `json:"files,omitempty"`
}

// ExtractRaw converts the Slack-specific content (blocks, attachments, files)
// from a message into a map for storage in the Raw field. Returns nil if the
// message has no extra content beyond text.
func NewSlackRawContent(msg goslack.Msg) SlackRawContent {
	if len(msg.Attachments) == 0 && len(msg.Blocks.BlockSet) == 0 && len(msg.Files) == 0 {
		return SlackRawContent{}
	}
	content := SlackRawContent{}
	if len(msg.Blocks.BlockSet) > 0 {
		content.Blocks = &msg.Blocks
	}
	if len(msg.Attachments) > 0 {
		content.Attachments = msg.Attachments
	}
	if len(msg.Files) > 0 {
		content.Files = msg.Files
	}
	return content
}

func (c *SlackRawContent) AsSerializable() SerailiableSlackRaw {
	data, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	var raw SerailiableSlackRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}
