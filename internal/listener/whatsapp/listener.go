package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish/claude-msg-utils/internal/store"
)

// Listener receives WhatsApp events and writes messages to local text files.
type Listener struct {
	client    *whatsmeow.Client
	account   string
	resolver  *Resolver
	syncing   atomic.Bool // true while history sync is in progress
	onLogout  func()      // called when device is unpaired remotely
	onMessage func(platform, account, conversation string) // called when a message is received
}

// New creates a WhatsApp listener for the given client and account directory name.
// onLogout is called when the device is unpaired from the phone (may be nil).
// onMessage is called when a message is received and written to disk (may be nil).
func New(client *whatsmeow.Client, account string, onLogout func(), onMessage func(string, string, string)) *Listener {
	return &Listener{
		client:    client,
		account:   account,
		resolver:  NewResolver(client),
		onLogout:  onLogout,
		onMessage: onMessage,
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
			slog.InfoContext(ctx, "whatsapp: connected", "account", l.account)
		case *events.Disconnected:
			slog.WarnContext(ctx, "whatsapp: disconnected", "account", l.account)
		}
	}
}

func (l *Listener) handleLoggedOut(ctx context.Context, evt *events.LoggedOut) {
	slog.WarnContext(ctx, "whatsapp: device logged out remotely",
		"account", l.account, "reason", evt.Reason.String())

	// Delete device data from whatsmeow's store.
	if err := l.client.Store.Delete(ctx); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to delete device store",
			"account", l.account, "error", err)
	}

	// Delete message data.
	dataDir := filepath.Join(store.DataDir(), "whatsapp", l.account)
	if err := os.RemoveAll(dataDir); err != nil {
		slog.ErrorContext(ctx, "whatsapp: failed to delete message data",
			"account", l.account, "error", err)
	} else {
		fmt.Printf("Deleted message data for %s (device was unlinked remotely).\n", l.account)
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

	text := ExtractText(evt.Message)
	if text == "" {
		return
	}

	senderName := l.resolver.ContactName(ctx, evt.Info.Sender)
	convDir := l.resolver.ConvDir(ctx, evt.Info.Chat)

	if err := store.WriteMessage("whatsapp", l.account, convDir, senderName, text, evt.Info.Timestamp); err != nil {
		slog.ErrorContext(ctx, "failed to write message", "error", err, "account", l.account)
		return
	}

	slog.InfoContext(ctx, "message saved",
		"from", senderName, "conv", convDir, "text_len", len(text))

	if l.onMessage != nil {
		l.onMessage("whatsapp", l.account, convDir)
	}
}
