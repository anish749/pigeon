package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Listener receives WhatsApp events and writes messages to local text files.
type Listener struct {
	client      *whatsmeow.Client
	acct        account.Account
	resolver    *Resolver
	store       store.Store
	syncing     atomic.Bool    // true while history sync is in progress
	onLogout    func()         // called when device is unpaired remotely
	onEvent     hub.NotifyFunc // called when a routable platform event is received
	syncTracker *syncstatus.Tracker
}

// New creates a WhatsApp listener for the given client and account.
// onLogout is called when the device is unpaired from the phone (may be nil).
// onEvent is called when a routable event (today: messages) has been
// written to disk. May be nil.
func New(client *whatsmeow.Client, acct account.Account, s store.Store, onLogout func(), onEvent hub.NotifyFunc, syncTracker *syncstatus.Tracker) *Listener {
	return &Listener{
		client:      client,
		acct:        acct,
		resolver:    NewResolver(client),
		store:       s,
		onLogout:    onLogout,
		onEvent:     onEvent,
		syncTracker: syncTracker,
	}
}

// Resolver returns the listener's name resolver.
func (l *Listener) Resolver() *Resolver { return l.resolver }

// Client returns the underlying whatsmeow client.
func (l *Listener) Client() *whatsmeow.Client { return l.client }

// EventHandler returns a function suitable for client.AddEventHandler.
func (l *Listener) EventHandler(ctx context.Context) func(any) {
	return func(evt any) {
		switch v := evt.(type) {
		case *events.Message:
			l.handleMessage(ctx, v)
		case *events.HistorySync:
			l.handleHistorySync(ctx, v)
		case *events.JoinedGroup:
			l.resolver.SetGroupName(v.JID, v.GroupInfo.GroupName.Name)
		case *events.GroupInfo:
			if v.Name != nil {
				l.resolver.SetGroupName(v.JID, v.Name.Name)
			}
		case *events.LoggedOut:
			l.handleLoggedOut(ctx, v)
		case *events.Connected:
			slog.InfoContext(ctx, "whatsapp: connected", "account", l.acct)
			go l.requestResyncIfNeeded(ctx)
		case *events.Disconnected:
			slog.WarnContext(ctx, "whatsapp: disconnected", "account", l.acct)
		}
	}
}

func (l *Listener) handleLoggedOut(ctx context.Context, evt *events.LoggedOut) {
	slog.WarnContext(ctx, "whatsapp: device logged out remotely",
		"account", l.acct, "reason", evt.Reason.String())

	// Delete device data from whatsmeow's store.
	if err := l.client.Store.Delete(ctx); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to delete device store",
			"account", l.acct, "error", err)
	}

	// Delete message data.
	dataDir := paths.DefaultDataRoot().AccountFor(l.acct).Path()
	if err := os.RemoveAll(dataDir); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to delete message data",
			"account", l.acct, "error", err)
	} else {
		fmt.Printf("Deleted message data for %s (device was unlinked remotely).\n", l.acct.Display())
	}

	// Notify the daemon to clean up config.
	if l.onLogout != nil {
		l.onLogout()
	}
}

func (l *Listener) handleMessage(ctx context.Context, evt *events.Message) {
	if l.client.Store.ID == nil {
		return
	}

	// While history sync is running, skip real-time messages — they'll already
	// be in the sync data, and writing them here risks duplicates (the contact
	// store may not have names yet, producing a different sender string).
	if l.syncing.Load() {
		return
	}

	// Skip broadcasts, newsletters, and self-messages.
	switch evt.Info.Chat.Server {
	case types.BroadcastServer, types.NewsletterServer:
		return
	}
	if evt.Info.Sender.User == l.client.Store.ID.User {
		return
	}

	// Edits and deletes (revokes) both ride the same *events.Message channel
	// as inner ProtocolMessages. Live edits are NOT consistently wrapped in
	// an EditedMessage envelope (which is what would set evt.IsEdit), so we
	// detect both kinds the same way: by peeking at ProtocolMessage.Type.
	if origID, edited := EditedMessage(evt.Message); origID != "" {
		l.handleEdit(ctx, evt, origID, edited)
		return
	}
	if id := RevokedMessageID(evt.Message); id != "" {
		l.handleDelete(ctx, evt, id)
		return
	}

	text := ExtractText(evt.Message)
	if text == "" {
		return
	}

	senderName := l.resolver.ContactName(ctx, evt.Info.Sender)
	convDir := l.resolver.ConvDir(ctx, evt.Info.Chat)

	var displayName string
	if evt.Info.Chat.Server == types.GroupServer {
		displayName = l.resolver.GroupName(ctx, evt.Info.Chat)
	} else {
		displayName = l.resolver.ContactName(ctx, evt.Info.Chat)
	}
	meta := l.resolver.ConvMeta(ctx, evt.Info.Chat, displayName)
	if _, err := l.store.WriteMetaIfNotExists(l.acct, convDir, meta); err != nil {
		slog.WarnContext(ctx, "whatsapp: write meta failed", "conv", convDir, "error", err)
	}

	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       evt.Info.ID,
			Ts:       evt.Info.Timestamp,
			Sender:   senderName,
			SenderID: evt.Info.Sender.String(),
			Text:     text,
		},
	}
	if err := l.store.Append(l.acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to write message", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "message saved",
		"from", senderName, "conv", convDir, "text_len", len(text))

	// onEvent is nil only during the `pigeon setup-whatsapp` QR-pairing flow
	// (commands/setup_whatsapp.go), which runs standalone before the daemon
	// — there's no hub to notify. In daemon-managed listeners it is always set.
	if l.onEvent != nil {
		l.onEvent(hub.NewMsg(l.acct, convDir, *line.Msg))
	}
}

// handleEdit stores a WhatsApp message edit and routes it to the connected
// session. origID and edited are extracted from the inner ProtocolMessage
// by the caller.
func (l *Listener) handleEdit(ctx context.Context, evt *events.Message, origID string, edited *waE2E.Message) {
	text := ExtractText(edited)
	if text == "" {
		// Media-only edits don't carry text; nothing meaningful to write.
		return
	}

	senderName := l.resolver.ContactName(ctx, evt.Info.Sender)
	convDir := l.resolver.ConvDir(ctx, evt.Info.Chat)

	line := modelv1.Line{
		Type: modelv1.LineEdit,
		Edit: &modelv1.EditLine{
			Ts:       evt.Info.Timestamp,
			MsgID:    origID,
			Sender:   senderName,
			SenderID: evt.Info.Sender.String(),
			Text:     text,
		},
	}
	if err := l.store.Append(l.acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to write edit", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "edit saved",
		"from", senderName, "conv", convDir, "msg_id", origID)

	if l.onEvent != nil {
		l.onEvent(hub.NewEdit(l.acct, convDir, *line.Edit))
	}
}

// handleDelete stores a WhatsApp message revoke and routes it to the
// connected session. origID is the target message's ID, already extracted
// from the protocol message by the caller.
func (l *Listener) handleDelete(ctx context.Context, evt *events.Message, origID string) {
	senderName := l.resolver.ContactName(ctx, evt.Info.Sender)
	convDir := l.resolver.ConvDir(ctx, evt.Info.Chat)

	line := modelv1.Line{
		Type: modelv1.LineDelete,
		Delete: &modelv1.DeleteLine{
			Ts:       evt.Info.Timestamp,
			MsgID:    origID,
			Sender:   senderName,
			SenderID: evt.Info.Sender.String(),
		},
	}
	if err := l.store.Append(l.acct, convDir, line); err != nil {
		slog.ErrorContext(ctx, "failed to write delete", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "delete saved",
		"from", senderName, "conv", convDir, "msg_id", origID)

	if l.onEvent != nil {
		l.onEvent(hub.NewDelete(l.acct, convDir, *line.Delete))
	}
}
