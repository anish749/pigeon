package model

import (
	"encoding/json"

	goslack "github.com/slack-go/slack"
)

type SerializableSlackRaw map[string]any

// Marshal the slack-go structs to JSON, then unmarshal back to map[string]any.
// This decouples the storage format from the slack-go types.
type SlackRawContent struct {
	Blocks      *goslack.Blocks      `json:"blocks,omitempty"`
	Attachments []goslack.Attachment `json:"attachments,omitempty"`
	Files       []goslack.File       `json:"files,omitempty"`
}

// NewSlackRawContent extracts Slack-specific content (blocks, attachments,
// files) from a message into a typed struct. Returns an empty struct if the
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

func (c SlackRawContent) AsSerializable() SerializableSlackRaw {
	if c.Blocks == nil && len(c.Attachments) == 0 && len(c.Files) == 0 {
		return nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	var raw SerializableSlackRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}

func FromSerializable(raw map[string]any) (SlackRawContent, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return SlackRawContent{}, err
	}
	var rc SlackRawContent
	if err := json.Unmarshal(data, &rc); err != nil {
		return SlackRawContent{}, err
	}
	return rc, nil
}
