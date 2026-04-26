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
	ms, err := NewMessageStore(acct, s)
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

func TestBotParticipatesInThread_BotIsParent(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", botUID, "hello", nil)
	if err := s.AppendThread(acct, "#general", "P1", parent); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}
	if !ms.BotParticipatesInThread("#general", "P1", botUID) {
		t.Error("expected participation when bot is parent sender")
	}
}

func TestBotParticipatesInThread_BotIsReplyAuthor(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", userUID, "hello", nil)
	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: time.Unix(1700000010, 0), Sender: "bot",
			SenderID: botUID, Text: "thanks", Reply: true,
		},
	}
	s.AppendThread(acct, "#general", "P1", parent)
	s.AppendThread(acct, "#general", "P1", reply)
	if !ms.BotParticipatesInThread("#general", "P1", botUID) {
		t.Error("expected participation when bot is reply author")
	}
}

func TestBotParticipatesInThread_BotMentionedInParentRaw(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	// Parent text is the resolved form (no <@UID>); the raw blocks carry
	// the user_id of the mention. This is the on-disk reality.
	parent := msg("P1", userUID, "@Bot hi", mentionRaw(botUID))
	s.AppendThread(acct, "#general", "P1", parent)
	if !ms.BotParticipatesInThread("#general", "P1", botUID) {
		t.Error("expected participation when bot mentioned in parent raw blocks")
	}
}

func TestBotParticipatesInThread_BotMentionedInReplyRaw(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", userUID, "talking about something", nil)
	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: time.Unix(1700000010, 0), Sender: "human",
			SenderID: userUID, Text: "@Bot ping", Reply: true,
			RawType: modelv1.RawTypeSlack, Raw: mentionRaw(botUID),
		},
	}
	s.AppendThread(acct, "#general", "P1", parent)
	s.AppendThread(acct, "#general", "P1", reply)
	if !ms.BotParticipatesInThread("#general", "P1", botUID) {
		t.Error("expected participation when bot mentioned in reply raw blocks")
	}
}

func TestBotParticipatesInThread_NoParticipation(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", userUID, "hello", nil)
	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: time.Unix(1700000010, 0), Sender: "other",
			SenderID: "U0OTHER01", Text: "ack", Reply: true,
		},
	}
	s.AppendThread(acct, "#general", "P1", parent)
	s.AppendThread(acct, "#general", "P1", reply)
	if ms.BotParticipatesInThread("#general", "P1", botUID) {
		t.Error("expected no participation when bot is absent")
	}
}

func TestBotParticipatesInThread_ThreadFileMissing(t *testing.T) {
	ms, _, _ := newTestMessageStore(t)
	if ms.BotParticipatesInThread("#general", "missing-ts", botUID) {
		t.Error("expected false when thread file does not exist")
	}
}

func TestBotParticipatesInThread_EmptyBotUID(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", "U0SOMEONE", "hi", mentionRaw("U0SOMEONE"))
	s.AppendThread(acct, "#general", "P1", parent)
	if ms.BotParticipatesInThread("#general", "P1", "") {
		t.Error("expected false when botUID is empty (would otherwise match U0SOMEONE)")
	}
}

// TestBotParticipatesInThread_DistinguishesUIDPrefix guards against the JSON
// substring match accidentally treating a UID prefix as a hit (e.g. botUID
// "U0BOT" matching a stored "user_id":"U0BOT0001").
func TestBotParticipatesInThread_DistinguishesUIDPrefix(t *testing.T) {
	ms, s, acct := newTestMessageStore(t)
	parent := msg("P1", userUID, "hi", mentionRaw("U0BOT0001XX"))
	s.AppendThread(acct, "#general", "P1", parent)
	if ms.BotParticipatesInThread("#general", "P1", "U0BOT0001") {
		t.Error("UID prefix collision: expected no participation")
	}
}
