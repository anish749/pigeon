package hub

import (
	"strings"
	"testing"
	"time"

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
