package slack

import (
	"testing"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestPigeonSendMetadata(t *testing.T) {
	tests := []struct {
		name string
		via  modelv1.Via
	}{
		{"pigeon-as-bot", modelv1.ViaPigeonAsBot},
		{"pigeon-as-user", modelv1.ViaPigeonAsUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := PigeonSendMetadata(tt.via)
			if md.EventType != pigeonSendEventType {
				t.Errorf("EventType = %q, want %q", md.EventType, pigeonSendEventType)
			}
			if v, ok := md.EventPayload["via"].(string); !ok || v != string(tt.via) {
				t.Errorf("EventPayload[via] = %v, want %q", md.EventPayload["via"], tt.via)
			}
		})
	}
}

func TestDetermineVia(t *testing.T) {
	tests := []struct {
		name    string
		msg     goslack.Msg
		isBotDM bool
		want    modelv1.Via
	}{
		{
			name:    "pigeon-as-bot from metadata",
			msg:     goslack.Msg{Metadata: goslack.SlackMetadata{EventType: pigeonSendEventType, EventPayload: map[string]any{"via": "pigeon-as-bot"}}},
			isBotDM: false,
			want:    modelv1.ViaPigeonAsBot,
		},
		{
			name:    "pigeon-as-user from metadata",
			msg:     goslack.Msg{Metadata: goslack.SlackMetadata{EventType: pigeonSendEventType, EventPayload: map[string]any{"via": "pigeon-as-user"}}},
			isBotDM: false,
			want:    modelv1.ViaPigeonAsUser,
		},
		{
			name:    "metadata wins over bot DM",
			msg:     goslack.Msg{Metadata: goslack.SlackMetadata{EventType: pigeonSendEventType, EventPayload: map[string]any{"via": "pigeon-as-bot"}}},
			isBotDM: true,
			want:    modelv1.ViaPigeonAsBot,
		},
		{
			name:    "bot DM without metadata",
			msg:     goslack.Msg{},
			isBotDM: true,
			want:    modelv1.ViaToPigeon,
		},
		{
			name:    "organic message",
			msg:     goslack.Msg{},
			isBotDM: false,
			want:    modelv1.ViaOrganic,
		},
		{
			name:    "unrelated metadata ignored",
			msg:     goslack.Msg{Metadata: goslack.SlackMetadata{EventType: "other_event"}},
			isBotDM: false,
			want:    modelv1.ViaOrganic,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineVia(tt.msg, tt.isBotDM)
			if got != tt.want {
				t.Errorf("DetermineVia() = %q, want %q", got, tt.want)
			}
		})
	}
}

