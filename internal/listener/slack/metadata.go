package slack

import (
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

// viaFromMetadata extracts the pigeon via field from Slack message metadata.
// Returns ViaOrganic if the message was not sent by pigeon.
func viaFromMetadata(md goslack.SlackMetadata) modelv1.Via {
	if md.EventType != pigeonSendEventType {
		return modelv1.ViaOrganic
	}
	if v, ok := md.EventPayload["via"].(string); ok {
		return modelv1.Via(v)
	}
	return modelv1.ViaOrganic
}
