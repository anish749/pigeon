package hub

import (
	"context"
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

// TestDrainConversation_IncludesThreadReplies verifies that thread-only replies
// (not broadcast to the channel) are delivered via drainConversation. This is
// the bug described in #254: thread replies are written to thread files, but
// the hub's drain reads via ReadConversation — which must interleave them.
func TestDrainConversation_IncludesThreadReplies(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
		store:    s,
		dataRoot: root,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Write a parent message to the date file.
	now := time.Now()
	parent := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "P1", Ts: now.Add(-10 * time.Second),
			Sender: "Alice", SenderID: "U001", Text: "channel message",
		},
	}
	if err := s.Append(acct, "#general", parent); err != nil {
		t.Fatalf("Append parent: %v", err)
	}

	// Write a thread-only reply to the thread file (not broadcast to channel).
	threadReply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: now.Add(-5 * time.Second),
			Sender: "Bob", SenderID: "U002", Text: "thread-only reply", Reply: true,
		},
	}
	if err := s.AppendThread(acct, "#general", "P1", parent); err != nil {
		t.Fatalf("AppendThread parent: %v", err)
	}
	if err := s.AppendThread(acct, "#general", "P1", threadReply); err != nil {
		t.Fatalf("AppendThread reply: %v", err)
	}

	// Set up a session that captures delivered messages.
	var delivered []NotificationMsg
	sess := &Session{
		SessionID: "sess-1",
		CWD:       "/tmp",
		Send: func(_ context.Context, msg NotificationMsg) error {
			delivered = append(delivered, msg)
			return nil
		},
		Ready: make(chan struct{}),
	}
	close(sess.Ready)
	h.sessions["sess-1"] = sess

	ch := &channel{
		acct:      acct,
		sessionID: "sess-1",
		signal:    make(chan deliverySignal, signalBufferSize),
	}

	// Drain with lastDelivered from 1 minute ago — both messages should be in range.
	lastDelivered := now.Add(-1 * time.Minute)
	h.drainConversation(ch, "#general", lastDelivered)

	if len(delivered) == 0 {
		t.Fatal("drainConversation delivered nothing — thread reply was lost")
	}

	// The delivered content should contain the thread reply text.
	content := delivered[0].Content()
	if !strings.Contains(content, "thread-only reply") {
		t.Errorf("delivered content missing thread reply.\ngot:\n%s", content)
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

	got, threadTS := h.lookupMessage(acct, "#general", "1700000001.000001")
	if got == nil {
		t.Fatal("lookupMessage returned nil, want match")
	}
	if got.Sender != "Alice" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Alice")
	}
	if got.Text != "hello world" {
		t.Errorf("Text = %q, want %q", got.Text, "hello world")
	}
	if threadTS != "" {
		t.Errorf("threadTS = %q, want empty for top-level message", threadTS)
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

	got, threadTS := h.lookupMessage(acct, "#general", "1700000002.000002")
	if got == nil {
		t.Fatal("lookupMessage returned nil, want match")
	}
	if got.Sender != "Bob" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Bob")
	}
	if got.Text != "thread reply" {
		t.Errorf("Text = %q, want %q", got.Text, "thread reply")
	}
	if threadTS != "1700000001.000001" {
		t.Errorf("threadTS = %q, want %q", threadTS, "1700000001.000001")
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

	got, _ := h.lookupMessage(acct, "#general", "9999999999.999999")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLookupMessage_NoConversation(t *testing.T) {
	h, _, acct := setupLookup(t)

	got, _ := h.lookupMessage(acct, "#nonexistent", "1700000001.000001")
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

// TestRouteEdit_DeliversToSession verifies that RouteEdit pushes a formatted
// edit notification to the connected session.
func TestRouteEdit_DeliversToSession(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
		store:    s,
		dataRoot: root,
		ctx:      ctx,
		cancel:   cancel,
	}

	var delivered []NotificationMsg
	sess := &Session{
		SessionID: "sess-edit",
		CWD:       "/tmp",
		Send: func(_ context.Context, msg NotificationMsg) error {
			delivered = append(delivered, msg)
			return nil
		},
		Ready: make(chan struct{}),
	}
	close(sess.Ready)
	h.sessions["sess-edit"] = sess

	ch := &channel{
		acct:      acct,
		sessionID: "sess-edit",
		signal:    make(chan deliverySignal, signalBufferSize),
	}
	h.channels[acct.String()] = ch
	go h.deliveryLoop(ch, time.Time{})

	edit := modelv1.EditLine{
		Ts: time.Now(), MsgID: "M1", ThreadTS: "P1",
		Sender: "Alice", SenderID: "U001", Text: "edited text",
	}
	if res := h.RouteEdit(acct, "#general", edit); res.State != RouteOK {
		t.Fatalf("RouteEdit state = %v, want RouteOK", res.State)
	}

	// Wait for the delivery loop to drain the signal.
	deadline := time.After(2 * time.Second)
	for {
		if len(delivered) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("RouteEdit did not deliver within 2s")
		case <-time.After(10 * time.Millisecond):
		}
	}

	content := delivered[0].Content()
	for _, want := range []string{"[edit]", "[message_id:M1]", "[thread_ts:P1]", "edited text"} {
		if !strings.Contains(content, want) {
			t.Errorf("delivered content missing %q:\n%s", want, content)
		}
	}
}

func TestRouteDelete_DeliversToSession(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
		store:    s,
		dataRoot: root,
		ctx:      ctx,
		cancel:   cancel,
	}

	var delivered []NotificationMsg
	sess := &Session{
		SessionID: "sess-del",
		CWD:       "/tmp",
		Send: func(_ context.Context, msg NotificationMsg) error {
			delivered = append(delivered, msg)
			return nil
		},
		Ready: make(chan struct{}),
	}
	close(sess.Ready)
	h.sessions["sess-del"] = sess

	ch := &channel{
		acct:      acct,
		sessionID: "sess-del",
		signal:    make(chan deliverySignal, signalBufferSize),
	}
	h.channels[acct.String()] = ch
	go h.deliveryLoop(ch, time.Time{})

	del := modelv1.DeleteLine{
		Ts: time.Now(), MsgID: "M2", ThreadTS: "P1",
		Sender: "Alice", SenderID: "U001",
	}
	if res := h.RouteDelete(acct, "#general", del); res.State != RouteOK {
		t.Fatalf("RouteDelete state = %v, want RouteOK", res.State)
	}

	deadline := time.After(2 * time.Second)
	for {
		if len(delivered) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("RouteDelete did not deliver within 2s")
		case <-time.After(10 * time.Millisecond):
		}
	}

	content := delivered[0].Content()
	for _, want := range []string{"[delete]", "[message_id:M2]", "[thread_ts:P1]"} {
		if !strings.Contains(content, want) {
			t.Errorf("delivered content missing %q:\n%s", want, content)
		}
	}
}

func TestRouteEdit_NoSessionRegistered(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
		store:    s,
		dataRoot: root,
		ctx:      ctx,
		cancel:   cancel,
	}
	res := h.RouteEdit(acct, "#general", modelv1.EditLine{MsgID: "M1"})
	if res.State != RouteNoSession {
		t.Errorf("State = %v, want RouteNoSession", res.State)
	}
}
