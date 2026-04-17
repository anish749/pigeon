package slack

import (
	"encoding/json"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

const pigeonSendEventType = "pigeon_send"

// PigeonSendMetadata returns Slack message metadata that tags a message as
// sent by pigeon with the given identity (pigeon-as-bot or pigeon-as-user).
func PigeonSendMetadata(via modelv1.Via) goslack.SlackMetadata {
	return goslack.SlackMetadata{
		EventType:    pigeonSendEventType,
		EventPayload: map[string]any{"via": string(via)},
	}
}

// DetermineVia returns the via identity for an incoming Slack message.
// Pigeon-sent messages carry metadata with the via field. Messages sent
// to the pigeon bot (isBotDM=true) that lack pigeon metadata are ViaToPigeon.
func DetermineVia(msg goslack.Msg, isBotDM bool) modelv1.Via {
	if msg.Metadata.EventType == pigeonSendEventType {
		if v, ok := msg.Metadata.EventPayload["via"].(string); ok {
			return modelv1.Via(v)
		}
	}
	if isBotDM {
		return modelv1.ViaToPigeon
	}
	return modelv1.ViaOrganic
}

// ExtractRaw converts the Slack-specific content (blocks, attachments, files)
// from a message into a map for storage in the Raw field. Returns nil if the
// message has no extra content beyond text.
func ExtractRaw(msg goslack.Msg) map[string]any {
	if len(msg.Attachments) == 0 && len(msg.Blocks.BlockSet) == 0 && len(msg.Files) == 0 {
		return nil
	}
	// Marshal the slack-go structs to JSON, then unmarshal back to map[string]any.
	// This decouples the storage format from the slack-go types.
	type rawContent struct {
		Blocks      *goslack.Blocks      `json:"blocks,omitempty"`
		Attachments []goslack.Attachment `json:"attachments,omitempty"`
		Files       []goslack.File       `json:"files,omitempty"`
	}
	content := rawContent{}
	if len(msg.Blocks.BlockSet) > 0 {
		content.Blocks = &msg.Blocks
	}
	if len(msg.Attachments) > 0 {
		content.Attachments = msg.Attachments
	}
	if len(msg.Files) > 0 {
		content.Files = msg.Files
	}
	data, err := json.Marshal(content)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}
