package whatsapp

import (
	"context"
	"log/slog"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish/claude-msg-utils/internal/store"
)

// Listener receives WhatsApp events and writes messages to local text files.
type Listener struct {
	client  *whatsmeow.Client
	account string
}

// New creates a WhatsApp listener for the given client and account directory name.
func New(client *whatsmeow.Client, account string) *Listener {
	return &Listener{client: client, account: account}
}

// EventHandler returns a function suitable for client.AddEventHandler.
func (l *Listener) EventHandler(ctx context.Context) func(any) {
	return func(evt any) {
		switch v := evt.(type) {
		case *events.Message:
			l.handleMessage(ctx, v)
		case *events.Connected:
			slog.InfoContext(ctx, "whatsapp: connected", "account", l.account)
		case *events.Disconnected:
			slog.WarnContext(ctx, "whatsapp: disconnected", "account", l.account)
		}
	}
}

func (l *Listener) handleMessage(ctx context.Context, evt *events.Message) {
	if l.client.Store.ID == nil {
		return
	}

	// Skip broadcast and self-messages
	if evt.Info.Chat.Server == "broadcast" {
		return
	}
	if evt.Info.Sender.User == l.client.Store.ID.User {
		return
	}

	text := ExtractText(evt.Message)
	if text == "" {
		return
	}

	senderJID := evt.Info.Sender
	// Resolve LID to phone JID if needed
	if senderJID.Server == types.HiddenUserServer {
		pnJID, err := l.client.Store.LIDs.GetPNForLID(ctx, senderJID)
		if err == nil && !pnJID.IsEmpty() {
			senderJID = pnJID
		}
	}

	// Build conversation directory name: +phone_PushName
	phone := "+" + senderJID.User
	pushName := evt.Info.PushName
	if pushName == "" {
		pushName = senderJID.User
	}
	pushName = SanitizeFilename(pushName)
	convDir := phone + "_" + pushName

	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = phone
	}

	if err := store.WriteMessage("whatsapp", l.account, convDir, senderName, text, evt.Info.Timestamp); err != nil {
		slog.ErrorContext(ctx, "failed to write message", "error", err, "account", l.account)
		return
	}

	slog.InfoContext(ctx, "message saved",
		"from", senderName, "conv", convDir, "text_len", len(text))
}
