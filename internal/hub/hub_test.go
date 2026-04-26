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
	root := paths.NewDataRoot(t.TempDir())
	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
		dataRoot: root,
	}
	got := h.ConnectedClaudeSessions()
	if len(got) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(got))
	}
}

func TestConnectedClaudeSessions_SingleSession(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
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
		dataRoot: root,
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
	root := paths.NewDataRoot(t.TempDir())
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
		dataRoot: root,
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
	root := paths.NewDataRoot(t.TempDir())
	h := &Hub{
		sessions: map[string]*Session{
			"sess-orphan": {SessionID: "sess-orphan", CWD: "/tmp"},
		},
		channels: make(map[string]*channel),
		dataRoot: root,
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

// TestDeliverEvent_MsgDirectPushNoDiskRead verifies the live path does
// not read disk for the event payload: the typed MsgLine handed to
// deliverEvent is formatted and pushed in one shot. The on-disk store is
// intentionally empty — if the implementation regressed to a drain-style
// disk read, the session would receive nothing.
func TestDeliverEvent_MsgDirectPushNoDiskRead(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
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
		SessionID: "sess-1",
		CWD:       "/tmp",
		Send: func(_ context.Context, m NotificationMsg) error {
			delivered = append(delivered, m)
			return nil
		},
		Ready: make(chan struct{}),
	}
	close(sess.Ready)
	h.sessions["sess-1"] = sess
	ch := &channel{acct: acct, sessionID: "sess-1"}

	now := time.Now()
	evt := NotifMsg{
		Envelope: Envelope{Kind: EventMessage, Account: acct, Conversation: "#general"},
		MsgLine: modelv1.MsgLine{
			ID: "M1", Ts: now, Sender: "Alice", SenderID: "U001",
			Text: "live arrival",
		},
	}
	got, ok := h.deliverEvent(ch, evt)

	if !ok {
		t.Fatal("deliverEvent returned ok=false on a healthy message push")
	}
	if got.Before(now) {
		t.Errorf("returned cursor %v should not predate the message ts %v", got, now)
	}
	if len(delivered) != 1 {
		t.Fatalf("delivered %d notifications, want 1", len(delivered))
	}
	content := delivered[0].Content()
	if !strings.Contains(content, "live arrival") {
		t.Errorf("notification missing payload text:\n%s", content)
	}
	if !strings.Contains(content, "[message_id:M1]") {
		t.Errorf("notification missing message_id tag:\n%s", content)
	}
}

// TestDeliverEvent_NoSessionDefersCursor verifies that when no session is
// connected, the live push reports failure (ok=false) so the caller
// leaves lastDelivered untouched. The next signalConnected drain will
// re-deliver from disk.
func TestDeliverEvent_NoSessionDefersCursor(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
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
	// Channel exists but no session is registered for it.
	ch := &channel{acct: acct, sessionID: "missing-sess"}

	evt := NotifMsg{
		Envelope: Envelope{Kind: EventMessage, Account: acct, Conversation: "#general"},
		MsgLine: modelv1.MsgLine{
			ID: "M1", Ts: time.Now(), Sender: "Alice", SenderID: "U001", Text: "x",
		},
	}
	got, ok := h.deliverEvent(ch, evt)
	if ok {
		t.Errorf("expected ok=false when session is not connected, got cursor %v", got)
	}
	if !got.IsZero() {
		t.Errorf("expected zero cursor on no-session, got %v", got)
	}
}

// TestRoute_FiresEventSignal verifies Route wraps the MsgLine in a
// NotifMsg event and enqueues a signalEvent carrying it.
func TestRoute_FiresEventSignal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Hub{
		sessions:  make(map[string]*Session),
		channels:  make(map[string]*channel),
		store:     s,
		dataRoot:  root,
		broadcast: NewBroadcast(),
		ctx:       ctx,
		cancel:    cancel,
	}
	ch := &channel{
		acct:      acct,
		sessionID: "sess-1",
		signal:    make(chan deliverySignal, signalBufferSize),
	}
	h.channels[acct.String()] = ch

	msg := modelv1.MsgLine{
		ID: "M1", Ts: time.Now(), Sender: "Alice", SenderID: "U001", Text: "hi",
	}
	if res := h.RouteEvent(NewMsg(acct, "#general", msg)); res.State != RouteOK {
		t.Fatalf("RouteEvent state = %v, want RouteOK", res.State)
	}

	select {
	case sig := <-ch.signal:
		if sig.kind != signalEvent {
			t.Errorf("signal.kind = %v, want signalEvent", sig.kind)
		}
		nm, ok := sig.event.(NotifMsg)
		if !ok {
			t.Fatalf("signal.event = %T, want NotifMsg", sig.event)
		}
		if nm.Conversation != "#general" {
			t.Errorf("envelope.Conversation = %q, want #general", nm.Conversation)
		}
		if nm.MsgLine.ID != "M1" || nm.MsgLine.Text != "hi" {
			t.Errorf("MsgLine = %+v, want M1/hi round-trip", nm.MsgLine)
		}
	default:
		t.Fatal("RouteEvent did not enqueue any signal")
	}
}

// TestNewReact_FiresEventSignal verifies the NewReact constructor picks
// EventReaction or EventUnreact based on ReactLine.Remove and that
// RouteEvent enqueues the resulting NotifReact via signalEvent.
func TestNewReact_FiresEventSignal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Hub{
		sessions:  make(map[string]*Session),
		channels:  make(map[string]*channel),
		store:     s,
		dataRoot:  root,
		broadcast: NewBroadcast(),
		ctx:       ctx,
		cancel:    cancel,
	}
	ch := &channel{
		acct:      acct,
		sessionID: "sess-1",
		signal:    make(chan deliverySignal, signalBufferSize),
	}
	h.channels[acct.String()] = ch

	tests := []struct {
		name     string
		react    modelv1.ReactLine
		wantKind EventKind
	}{
		{"add", modelv1.ReactLine{MsgID: "M1", Sender: "B", SenderID: "U2", Emoji: "x"}, EventReaction},
		{"remove", modelv1.ReactLine{MsgID: "M1", Sender: "B", SenderID: "U2", Emoji: "x", Remove: true}, EventUnreact},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if res := h.RouteEvent(NewReact(acct, "#general", tt.react)); res.State != RouteOK {
				t.Fatalf("RouteEvent state = %v, want RouteOK", res.State)
			}
			select {
			case sig := <-ch.signal:
				if sig.kind != signalEvent {
					t.Errorf("signal.kind = %v, want signalEvent", sig.kind)
				}
				nr, ok := sig.event.(NotifReact)
				if !ok {
					t.Fatalf("signal.event = %T, want NotifReact", sig.event)
				}
				if nr.Kind != tt.wantKind {
					t.Errorf("envelope.Kind = %q, want %q", nr.Kind, tt.wantKind)
				}
			default:
				t.Fatal("RouteReaction did not enqueue any signal")
			}
		})
	}
}

// TestDeliverEvent_AdvancesCursor confirms the cursor return contract:
// messages advance, reactions don't.
func TestDeliverEvent_AdvancesCursor(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)
	root := paths.NewDataRoot(tmp)
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
	sess := &Session{
		SessionID: "sess-1",
		CWD:       "/tmp",
		Send:      func(_ context.Context, _ NotificationMsg) error { return nil },
		Ready:     make(chan struct{}),
	}
	close(sess.Ready)
	h.sessions["sess-1"] = sess
	ch := &channel{acct: acct, sessionID: "sess-1"}

	tests := []struct {
		name        string
		event       Notification
		wantAdvance bool
	}{
		{
			name: "message advances cursor",
			event: NotifMsg{
				Envelope: Envelope{Kind: EventMessage, Account: acct, Conversation: "#general"},
				MsgLine:  modelv1.MsgLine{ID: "M1", Ts: time.Now(), Sender: "A", SenderID: "U1", Text: "hi"},
			},
			wantAdvance: true,
		},
		{
			name: "reaction does not advance cursor",
			event: NotifReact{
				Envelope:  Envelope{Kind: EventReaction, Account: acct, Conversation: "#general"},
				ReactLine: modelv1.ReactLine{MsgID: "M1", Sender: "B", SenderID: "U2", Emoji: "thumbsup"},
			},
			wantAdvance: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := h.deliverEvent(ch, tt.event)
			if ok != tt.wantAdvance {
				t.Errorf("deliverEvent ok = %v, want %v (cursor=%v)", ok, tt.wantAdvance, got)
			}
			if !tt.wantAdvance && !got.IsZero() {
				t.Errorf("expected zero cursor when advance=false, got %v", got)
			}
		})
	}
}
