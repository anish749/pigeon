package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// handleHistorySync processes a history sync event from whatsmeow, writing all
// messages to the local text-file store. WhatsApp sends these events when a new
// device is linked, delivering historical conversations in chunks.
//
// Names are saved to the contact store first so that both this handler and the
// real-time listener resolve names identically via the shared Resolver.
func (l *Listener) handleHistorySync(ctx context.Context, evt *events.HistorySync) {
	data := evt.Data
	if data == nil {
		return
	}

	// Suppress real-time message writes while syncing to avoid duplicates.
	l.syncing.Store(true)
	defer l.syncing.Store(false)

	statusKey := l.acct.Display()
	l.syncTracker.Start(statusKey)
	var syncErr error
	defer func() { l.syncTracker.Done(statusKey, syncErr) }()

	syncType := data.GetSyncType().String()

	slog.InfoContext(ctx, "whatsapp: history sync received",
		"account", l.acct,
		"type", syncType,
		"conversations", len(data.GetConversations()),
		"pushnames", len(data.GetPushnames()),
	)

	// Phase 1: Persist all names so the resolver can find them.
	l.saveNamesFromSync(ctx, data)

	// Phase 2: Process messages using the shared resolver.
	var totalMessages int
	var errs []error
	for _, conv := range data.GetConversations() {
		if conv.GetID() == "" {
			continue
		}
		n, err := l.syncConversation(ctx, conv)
		totalMessages += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	syncErr = errors.Join(errs...)

	slog.InfoContext(ctx, "whatsapp: history sync complete",
		"account", l.acct,
		"type", syncType,
		"messages_written", totalMessages,
	)
}

// saveNamesFromSync persists push names, contact display names, and group names
// from a history sync event so the resolver can use them.
func (l *Listener) saveNamesFromSync(ctx context.Context, data *waHistorySync.HistorySync) {
	// Save push names to the contact store.
	for _, pn := range data.GetPushnames() {
		if pn.GetID() == "" || pn.GetPushname() == "" {
			continue
		}
		jid, err := types.ParseJID(pn.GetID())
		if err != nil {
			continue
		}
		if _, _, err := l.client.Store.Contacts.PutPushName(ctx, jid, pn.GetPushname()); err != nil {
			slog.WarnContext(ctx, "whatsapp: history sync: failed to save push name",
				"jid", pn.GetID(), "error", err)
		}
	}

	// Save conversation-level names.
	for _, conv := range data.GetConversations() {
		if conv.GetID() == "" {
			continue
		}
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}

		switch {
		case chatJID.Server == types.GroupServer:
			// Cache group name for the resolver.
			if name := conv.GetName(); name != "" {
				l.resolver.SetGroupName(chatJID, name)
			}

		case chatJID.Server == types.BroadcastServer || chatJID.Server == types.NewsletterServer:
			// Skip.

		default:
			// DM: resolve LID if needed, save display name as contact name.
			resolved := chatJID
			if chatJID.Server == types.HiddenUserServer {
				if pnJID := conv.GetPnJID(); pnJID != "" {
					if parsed, err := types.ParseJID(pnJID); err == nil {
						resolved = parsed
					}
				}
			}
			if displayName := conv.GetDisplayName(); displayName != "" {
				if err := l.client.Store.Contacts.PutContactName(ctx, resolved, displayName, ""); err != nil {
					slog.WarnContext(ctx, "whatsapp: history sync: failed to save contact name",
						"jid", resolved.String(), "error", err)
				}
			}
		}
	}
}

// syncConversation writes all messages from a single history-sync conversation.
func (l *Listener) syncConversation(ctx context.Context, conv *waHistorySync.Conversation) (int, error) {
	chatJID, err := types.ParseJID(conv.GetID())
	if err != nil {
		return 0, fmt.Errorf("parse JID %s: %w", conv.GetID(), err)
	}

	// If conversation JID is a LID, resolve to phone JID.
	if chatJID.Server == types.HiddenUserServer {
		if pnJID := conv.GetPnJID(); pnJID != "" {
			if parsed, err := types.ParseJID(pnJID); err == nil {
				chatJID = parsed
			}
		}
	}

	// Skip broadcasts (including status) and newsletters.
	switch chatJID.Server {
	case types.BroadcastServer, types.NewsletterServer:
		return 0, nil
	}

	isGroup := chatJID.Server == types.GroupServer
	convDir := l.resolver.ConvDir(ctx, chatJID)

	displayName := convDir
	if isGroup {
		displayName = l.resolver.GroupName(ctx, chatJID)
	} else {
		displayName = l.resolver.ContactName(ctx, chatJID)
	}
	meta := l.resolver.ConvMeta(ctx, chatJID, displayName)
	if _, err := l.store.WriteMetaIfNotExists(l.acct, convDir, meta); err != nil {
		slog.WarnContext(ctx, "whatsapp: write meta failed", "conv", convDir, "error", err)
	}

	var written int
	var errs []error
	for _, hsMsg := range conv.GetMessages() {
		wmi := hsMsg.GetMessage()
		if wmi == nil || wmi.GetMessage() == nil {
			continue
		}

		text := ExtractText(wmi.GetMessage())
		if text == "" {
			continue
		}

		msgTS := wmi.GetMessageTimestamp()
		if msgTS == 0 {
			continue
		}
		ts := time.Unix(int64(msgTS), 0)

		senderName, senderID := l.resolveHistorySenderWithID(ctx, chatJID, wmi, isGroup)

		line := modelv1.Line{
			Type: modelv1.LineMessage,
			Msg: &modelv1.MsgLine{
				ID:       wmi.GetKey().GetID(),
				Ts:       ts,
				Sender:   senderName,
				SenderID: senderID,
				Text:     text,
			},
		}
		if err := l.store.Append(l.acct, convDir, line); err != nil {
			slog.ErrorContext(ctx, "whatsapp: history sync: write failed",
				"error", err, "account", l.acct, "conv", convDir)
			errs = append(errs, err)
			continue
		}
		written++
	}

	if written > 0 {
		slog.InfoContext(ctx, "whatsapp: history sync: conversation done",
			"conv", convDir, "messages", written, "account", l.acct)
	}

	return written, errors.Join(errs...)
}

// resolveHistorySender returns the display name for a history sync message sender.
// Uses the shared resolver (backed by the contact store) for consistency with real-time.
func (l *Listener) resolveHistorySender(ctx context.Context, chatJID types.JID, wmi *waWeb.WebMessageInfo, isGroup bool) string {
	name, _ := l.resolveHistorySenderWithID(ctx, chatJID, wmi, isGroup)
	return name
}

// resolveHistorySenderWithID returns both the display name and the JID string for a history sync sender.
func (l *Listener) resolveHistorySenderWithID(ctx context.Context, chatJID types.JID, wmi *waWeb.WebMessageInfo, isGroup bool) (string, string) {
	key := wmi.GetKey()

	if key.GetFromMe() {
		if l.client.Store.ID != nil {
			jid := *l.client.Store.ID
			return l.resolver.ContactName(ctx, jid), jid.String()
		}
		return "me", ""
	}

	// Determine sender JID.
	var senderJIDStr string
	if isGroup {
		senderJIDStr = wmi.GetParticipant()
		if senderJIDStr == "" {
			senderJIDStr = key.GetParticipant()
		}
	} else {
		senderJIDStr = key.GetRemoteJID()
	}

	if senderJIDStr == "" {
		return l.resolver.ContactName(ctx, chatJID), chatJID.String()
	}

	senderJID, err := types.ParseJID(senderJIDStr)
	if err != nil {
		return senderJIDStr, senderJIDStr
	}

	return l.resolver.ContactName(ctx, senderJID), senderJID.String()
}
