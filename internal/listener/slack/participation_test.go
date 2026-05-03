package slack

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func newTestMessageStore(t *testing.T) (*MessageStore, *store.FSStore, account.Account) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")
	ms, err := NewMessageStore(acct, s, func(account.Account) {})
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	return ms, s, acct
}

const (
	botUID  = "U0BOT0001"
	userUID = "U0HUMAN01"
)

func msg(id, senderID, text string, raw map[string]any) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: id, Ts: time.Unix(1700000000, 0), Sender: "name", SenderID: senderID,
			Text: text, RawType: modelv1.RawTypeSlack, Raw: raw,
		},
	}
}

func reply(id, senderID, text string, raw map[string]any) modelv1.Line {
	l := msg(id, senderID, text, raw)
	l.Msg.Reply = true
	l.Msg.Ts = time.Unix(1700000010, 0)
	return l
}

// mentionRaw is the on-disk shape of a Slack rich_text mention block,
// reproducing what the listener actually stores after JSON round-trip.
func mentionRaw(uid string) map[string]any {
	return map[string]any{
		"blocks": []any{
			map[string]any{
				"type": "rich_text",
				"elements": []any{
					map[string]any{
						"type": "rich_text_section",
						"elements": []any{
							map[string]any{"type": "user", "user_id": uid},
							map[string]any{"type": "text", "text": " hi"},
						},
					},
				},
			},
		},
	}
}

func TestBotParticipatesInThread(t *testing.T) {
	const threadTS = "P1"

	tests := []struct {
		name  string
		lines []modelv1.Line // empty => don't create thread file
		query string         // botUID passed to BotParticipatesInThread
		want  bool
	}{
		{
			name:  "bot is parent sender",
			lines: []modelv1.Line{msg("P1", botUID, "hello", nil)},
			query: botUID,
			want:  true,
		},
		{
			name: "bot is reply author",
			lines: []modelv1.Line{
				msg("P1", userUID, "hello", nil),
				reply("R1", botUID, "thanks", nil),
			},
			query: botUID,
			want:  true,
		},
		{
			name:  "bot mentioned in parent raw blocks",
			lines: []modelv1.Line{msg("P1", userUID, "@Bot hi", mentionRaw(botUID))},
			query: botUID,
			want:  true,
		},
		{
			name: "bot mentioned in reply raw blocks",
			lines: []modelv1.Line{
				msg("P1", userUID, "talking about something", nil),
				reply("R1", userUID, "@Bot ping", mentionRaw(botUID)),
			},
			query: botUID,
			want:  true,
		},
		{
			name: "no participation",
			lines: []modelv1.Line{
				msg("P1", userUID, "hello", nil),
				reply("R1", "U0OTHER01", "ack", nil),
			},
			query: botUID,
			want:  false,
		},
		{
			name:  "thread file missing",
			lines: nil,
			query: botUID,
			want:  false,
		},
		{
			name:  "empty botUID does not match anyone",
			lines: []modelv1.Line{msg("P1", "U0SOMEONE", "hi", mentionRaw("U0SOMEONE"))},
			query: "",
			want:  false,
		},
		{
			name: "UID prefix collision is not a hit",
			// Stored mention is for "U0BOT0001XX"; querying for "U0BOT0001"
			// must not match — the JSON substring check has to anchor on the
			// closing quote of the value.
			lines: []modelv1.Line{msg("P1", userUID, "hi", mentionRaw("U0BOT0001XX"))},
			query: "U0BOT0001",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, s, acct := newTestMessageStore(t)
			for _, line := range tt.lines {
				if err := s.AppendThread(acct, "#general", threadTS, line); err != nil {
					t.Fatalf("AppendThread: %v", err)
				}
			}
			got := ms.BotParticipatesInThread("#general", threadTS, tt.query)
			if got != tt.want {
				t.Errorf("BotParticipatesInThread = %v, want %v", got, tt.want)
			}
		})
	}
}
