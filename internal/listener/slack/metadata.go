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
