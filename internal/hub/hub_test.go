package hub

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestConnectedClaudeSessions_Empty(t *testing.T) {
	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
	}
	got := h.ConnectedClaudeSessions()
	if len(got) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(got))
	}
}

func TestConnectedClaudeSessions_SingleSession(t *testing.T) {
	h := &Hub{
		sessions: map[string]*Session{
			"sess-1": {SessionID: "sess-1", CWD: "/home/user/project"},
		},
		channels: map[string]*channel{
			"slack-acme": {
				acct:      account.New("slack", "Acme Corp"),
				sessionID: "sess-1",
			},
		},
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
	if got[0].SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", got[0].SessionID, "sess-1")
	}
	if got[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want %q", got[0].CWD, "/home/user/project")
	}
	if got[0].Account != "slack/Acme Corp" {
		t.Errorf("Account = %q, want %q", got[0].Account, "slack/Acme Corp")
	}
}

func TestConnectedClaudeSessions_MultipleSessions(t *testing.T) {
	h := &Hub{
		sessions: map[string]*Session{
			"sess-1": {SessionID: "sess-1", CWD: "/home/user/project-a"},
			"sess-2": {SessionID: "sess-2", CWD: "/home/user/project-b"},
		},
		channels: map[string]*channel{
			"slack-acme": {
				acct:      account.New("slack", "Acme Corp"),
				sessionID: "sess-1",
			},
			"whatsapp-phone": {
				acct:      account.New("whatsapp", "+1234567890"),
				sessionID: "sess-2",
			},
		},
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}

	// Sort for deterministic comparison since map iteration is unordered.
	sort.Slice(got, func(i, j int) bool { return got[i].SessionID < got[j].SessionID })

	if got[0].SessionID != "sess-1" || got[0].Account != "slack/Acme Corp" {
		t.Errorf("session[0] = %+v, want sess-1 / slack/Acme Corp", got[0])
	}
	if got[1].SessionID != "sess-2" || got[1].Account != "whatsapp/+1234567890" {
		t.Errorf("session[1] = %+v, want sess-2 / whatsapp/+1234567890", got[1])
	}
}

func TestConnectedClaudeSessions_SessionWithNoChannel(t *testing.T) {
	// A session exists but no channel points to it — Account should be empty.
	h := &Hub{
		sessions: map[string]*Session{
			"sess-orphan": {SessionID: "sess-orphan", CWD: "/tmp"},
		},
		channels: make(map[string]*channel),
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
	if got[0].Account != "" {
		t.Errorf("Account = %q, want empty for orphan session", got[0].Account)
	}
}

func setupLookup(t *testing.T) (*Hub, *store.FSStore, account.Account) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")
	h := &Hub{dataRoot: root}
	return h, s, acct
}

func TestLookupMessage_DateFile(t *testing.T) {
	h, s, acct := setupLookup(t)

	msg := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
			Sender: "Alice", SenderID: "U001", Text: "hello world",
		},
	}
	if err := s.Append(acct, "#general", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := h.lookupMessage(acct, "#general", "1700000001.000001")
	if got == nil {
		t.Fatal("lookupMessage returned nil, want match")
	}
	if got.Sender != "Alice" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Alice")
	}
	if got.Text != "hello world" {
		t.Errorf("Text = %q, want %q", got.Text, "hello world")
	}
}

func TestLookupMessage_ThreadFile(t *testing.T) {
	h, s, acct := setupLookup(t)

	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000002.000002", Ts: time.Date(2026, 4, 19, 10, 5, 0, 0, time.UTC),
			Sender: "Bob", SenderID: "U002", Text: "thread reply", Reply: true,
		},
	}
	if err := s.AppendThread(acct, "#general", "1700000001.000001", reply); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}

	got := h.lookupMessage(acct, "#general", "1700000002.000002")
	if got == nil {
		t.Fatal("lookupMessage returned nil, want match")
	}
	if got.Sender != "Bob" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Bob")
	}
	if got.Text != "thread reply" {
		t.Errorf("Text = %q, want %q", got.Text, "thread reply")
	}
}

func TestLookupMessage_NotFound(t *testing.T) {
	h, s, acct := setupLookup(t)

	msg := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
			Sender: "Alice", SenderID: "U001", Text: "hello",
		},
	}
	if err := s.Append(acct, "#general", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := h.lookupMessage(acct, "#general", "9999999999.999999")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLookupMessage_NoConversation(t *testing.T) {
	h, _, acct := setupLookup(t)

	got := h.lookupMessage(acct, "#nonexistent", "1700000001.000001")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestFormatReactionLines_MessageFound(t *testing.T) {
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

	react := modelv1.ReactLine{
		MsgID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
		Sender: "Bob", SenderID: "U002", Emoji: "thumbsup",
	}
	lines := h.formatReactionLines(acct, "#general", react)

	if len(lines) == 0 {
		t.Fatal("expected non-empty lines")
	}
	// Full notification includes the original message text.
	if !strings.Contains(lines[0], "hello world") {
		t.Errorf("first line %q should contain original message text", lines[0])
	}
	if !strings.Contains(lines[0], "Bob") {
		t.Errorf("first line %q should contain reactor name", lines[0])
	}
}

func TestFormatReactionLines_MessageNotFound(t *testing.T) {
	h, _, acct := setupLookup(t)

	react := modelv1.ReactLine{
		MsgID: "9999999999.999999", Ts: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
		Sender: "Bob", SenderID: "U002", Emoji: "thumbsup",
	}
	lines := h.formatReactionLines(acct, "#general", react)

	if len(lines) == 0 {
		t.Fatal("expected non-empty lines")
	}
	// Fallback notification omits original message text.
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
