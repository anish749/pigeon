package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// TestNotifReact_FormatNotification_MessageFound verifies the parent-found
// branch: when LookupParent returns a MsgLine, the rendered header
// includes the parent's text and sender. Exercises NotifReact's own
// formatting via the shared FormatEnv contract.
func TestNotifReact_FormatNotification_MessageFound(t *testing.T) {
	h, s, acct := setupLookup(t)

	if err := s.Append(acct, "#general", modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
			Sender: "Alice", SenderID: "U001", Text: "hello world",
		},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	evt := NotifReact{
		Envelope: Envelope{Kind: EventReaction, Account: acct, Conversation: "#general"},
		ReactLine: modelv1.ReactLine{
			MsgID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
			Sender: "Bob", SenderID: "U002", Emoji: "thumbsup",
		},
	}
	env := FormatEnv{
		Loc: time.UTC,
		LookupParent: func(id string) *modelv1.MsgLine {
			return h.lookupMessage(acct, "#general", id)
		},
	}
	lines := evt.FormatNotification(env)

	if len(lines) == 0 {
		t.Fatal("expected non-empty lines")
	}
	if !strings.Contains(lines[0], "hello world") {
		t.Errorf("first line %q should contain original message text", lines[0])
	}
	if !strings.Contains(lines[0], "Bob") {
		t.Errorf("first line %q should contain reactor name", lines[0])
	}
}

// TestNotifReact_FormatNotification_MessageNotFound verifies the fallback
// branch: when LookupParent returns nil, the renderer omits the parent's
// text and emits a context-less reaction line.
func TestNotifReact_FormatNotification_MessageNotFound(t *testing.T) {
	h, _, acct := setupLookup(t)

	evt := NotifReact{
		Envelope: Envelope{Kind: EventReaction, Account: acct, Conversation: "#general"},
		ReactLine: modelv1.ReactLine{
			MsgID: "9999999999.999999", Ts: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
			Sender: "Bob", SenderID: "U002", Emoji: "thumbsup",
		},
	}
	env := FormatEnv{
		Loc: time.UTC,
		LookupParent: func(id string) *modelv1.MsgLine {
			return h.lookupMessage(acct, "#general", id)
		},
	}
	lines := evt.FormatNotification(env)

	if len(lines) == 0 {
		t.Fatal("expected non-empty lines")
	}
	if strings.Contains(lines[0], "hello world") {
		t.Errorf("fallback line %q should not contain original message text", lines[0])
	}
	if !strings.Contains(lines[0], "Bob") {
		t.Errorf("fallback line %q should contain reactor name", lines[0])
	}
	if !strings.Contains(lines[0], "thumbsup") {
		t.Errorf("fallback line %q should contain emoji", lines[0])
	}
}

// TestConstructors_BuildEnvelopesWithRightKind covers all four typed
// constructors in one table. Each maps a payload to the matching EventKind
// and threads conversation + account through unchanged. NewReact has the
// extra job of picking EventReaction vs EventUnreact from ReactLine.Remove.
func TestConstructors_BuildEnvelopesWithRightKind(t *testing.T) {
	acct := account.New("slack", "acme-corp")
	conv := "#general"

	tests := []struct {
		name      string
		event     Notification
		wantKind  EventKind
		wantConv  string
		wantAdvCu bool
	}{
		{
			name:      "NewMsg",
			event:     NewMsg(acct, conv, modelv1.MsgLine{ID: "M1", Sender: "A", SenderID: "U1"}),
			wantKind:  EventMessage,
			wantConv:  conv,
			wantAdvCu: true,
		},
		{
			name:      "NewReact (add)",
			event:     NewReact(acct, conv, modelv1.ReactLine{MsgID: "M1", Emoji: "x"}),
			wantKind:  EventReaction,
			wantConv:  conv,
			wantAdvCu: false,
		},
		{
			name:      "NewReact (remove)",
			event:     NewReact(acct, conv, modelv1.ReactLine{MsgID: "M1", Emoji: "x", Remove: true}),
			wantKind:  EventUnreact,
			wantConv:  conv,
			wantAdvCu: false,
		},
		{
			name:      "NewEdit",
			event:     NewEdit(acct, conv, modelv1.EditLine{MsgID: "M1", Sender: "A", SenderID: "U1", Text: "x"}),
			wantKind:  EventEdit,
			wantConv:  conv,
			wantAdvCu: true,
		},
		{
			name:      "NewDelete",
			event:     NewDelete(acct, conv, modelv1.DeleteLine{MsgID: "M1", Sender: "A", SenderID: "U1"}),
			wantKind:  EventDelete,
			wantConv:  conv,
			wantAdvCu: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.event.envelope()
			if env.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", env.Kind, tt.wantKind)
			}
			if env.Conversation != tt.wantConv {
				t.Errorf("Conversation = %q, want %q", env.Conversation, tt.wantConv)
			}
			if env.Account != acct {
				t.Errorf("Account = %v, want %v", env.Account, acct)
			}
			if got := tt.event.AdvancesCursor(); got != tt.wantAdvCu {
				t.Errorf("AdvancesCursor() = %v, want %v", got, tt.wantAdvCu)
			}
		})
	}
}

// TestNotifEditDelete_FormatNotification covers the rendering path for
// edits and deletes through the FormatEnv contract — same shape as
// NotifMsg, no parent lookup needed.
func TestNotifEditDelete_FormatNotification(t *testing.T) {
	acct := account.New("slack", "acme-corp")
	conv := "#general"
	env := FormatEnv{Loc: time.UTC}

	editEvt := NewEdit(acct, conv, modelv1.EditLine{
		Ts:    time.Date(2026, 4, 26, 10, 30, 0, 0, time.UTC),
		MsgID: "M1", Sender: "Alice", SenderID: "U001", Text: "edited",
	})
	editLines := editEvt.FormatNotification(env)
	if len(editLines) < 2 || !strings.Contains(editLines[0], "edited") {
		t.Errorf("edit lines unexpected: %v", editLines)
	}
	if !strings.Contains(editLines[len(editLines)-1], "[edit]") {
		t.Errorf("edit meta missing [edit] tag: %q", editLines[len(editLines)-1])
	}

	delEvt := NewDelete(acct, conv, modelv1.DeleteLine{
		Ts:    time.Date(2026, 4, 26, 10, 31, 0, 0, time.UTC),
		MsgID: "M1", Sender: "Alice", SenderID: "U001",
	})
	delLines := delEvt.FormatNotification(env)
	if len(delLines) != 2 {
		t.Fatalf("delete lines = %d, want 2: %v", len(delLines), delLines)
	}
	if !strings.Contains(delLines[1], "[delete]") {
		t.Errorf("delete meta missing [delete] tag: %q", delLines[1])
	}
}
