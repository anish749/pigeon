package whatsapp

import (
	"context"
	"log/slog"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/anish/claude-msg-utils/internal/store"
)

// handleHistorySync processes a history sync event from whatsmeow, writing all
// messages to the local text-file store. WhatsApp sends these events when a new
// device is linked, delivering historical conversations in chunks.
func (l *Listener) handleHistorySync(ctx context.Context, evt *events.HistorySync) {
	data := evt.Data
	if data == nil {
		return
	}

	syncType := data.GetSyncType().String()

	slog.InfoContext(ctx, "whatsapp: history sync received",
		"account", l.account,
		"type", syncType,
		"conversations", len(data.GetConversations()),
		"pushnames", len(data.GetPushnames()),
	)

	// Build pushname lookup from sync metadata.
	pushnames := make(map[string]string)
	for _, pn := range data.GetPushnames() {
		if pn.GetID() != "" && pn.GetPushname() != "" {
			pushnames[pn.GetID()] = pn.GetPushname()
		}
	}

	var totalMessages int
	for _, conv := range data.GetConversations() {
		if conv.GetID() == "" {
			continue
		}
		totalMessages += l.syncConversation(ctx, conv, pushnames)
	}

	slog.InfoContext(ctx, "whatsapp: history sync complete",
		"account", l.account,
		"type", syncType,
		"messages_written", totalMessages,
	)
}

// syncConversation writes all messages from a single history-sync conversation.
func (l *Listener) syncConversation(ctx context.Context, conv *waHistorySync.Conversation, pushnames map[string]string) int {
	chatJID, err := types.ParseJID(conv.GetID())
	if err != nil {
		slog.WarnContext(ctx, "whatsapp: history sync: invalid JID",
			"jid", conv.GetID(), "error", err)
		return 0
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
	if chatJID.Server == types.BroadcastServer || chatJID.Server == types.NewsletterServer {
		return 0
	}

	isGroup := chatJID.Server == types.GroupServer
	convDir := l.buildConvDir(chatJID, conv, pushnames, isGroup)

	var written int
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

		senderName := l.resolveSender(ctx, chatJID, wmi, pushnames, isGroup)

		if err := store.WriteMessage("whatsapp", l.account, convDir, senderName, text, ts); err != nil {
			slog.ErrorContext(ctx, "whatsapp: history sync: write failed",
				"error", err, "account", l.account, "conv", convDir)
			continue
		}
		written++
	}

	if written > 0 {
		slog.InfoContext(ctx, "whatsapp: history sync: conversation done",
			"conv", convDir, "messages", written, "account", l.account)
	}

	return written
}

// buildConvDir returns the conversation directory name for file storage.
// DMs use "+phone_Name" (matching the real-time handler), groups use the group name.
func (l *Listener) buildConvDir(chatJID types.JID, conv *waHistorySync.Conversation, pushnames map[string]string, isGroup bool) string {
	if isGroup {
		name := conv.GetName()
		if name == "" {
			name = conv.GetDisplayName()
		}
		if name == "" {
			name = chatJID.User
		}
		return SanitizeFilename(name)
	}

	// DM: +phone_Name format
	phone := "+" + chatJID.User
	name := pushnames[conv.GetID()]
	if name == "" {
		name = conv.GetDisplayName()
	}
	if name == "" {
		name = conv.GetName()
	}
	if name == "" {
		name = chatJID.User
	}
	return phone + "_" + SanitizeFilename(name)
}

// resolveSender returns the display name for a message sender.
func (l *Listener) resolveSender(ctx context.Context, chatJID types.JID, wmi *waWeb.WebMessageInfo, pushnames map[string]string, isGroup bool) string {
	if wmi.GetKey().GetFromMe() {
		if l.client.Store.ID != nil {
			return "+" + l.client.Store.ID.User
		}
		return "me"
	}

	// Determine the sender JID string.
	var senderJIDStr string
	if isGroup {
		senderJIDStr = wmi.GetParticipant()
		if senderJIDStr == "" {
			senderJIDStr = wmi.GetKey().GetParticipant()
		}
	} else {
		senderJIDStr = wmi.GetKey().GetRemoteJID()
	}

	if senderJIDStr == "" {
		return "+" + chatJID.User
	}

	senderJID, err := types.ParseJID(senderJIDStr)
	if err != nil {
		return senderJIDStr
	}

	// Resolve LID to phone JID.
	if senderJID.Server == types.HiddenUserServer {
		pnJID, err := l.client.Store.LIDs.GetPNForLID(ctx, senderJID)
		if err == nil && !pnJID.IsEmpty() {
			senderJID = pnJID
		}
	}

	// Try: message push name → sync pushnames → phone number.
	if name := wmi.GetPushName(); name != "" {
		return name
	}
	if name, ok := pushnames[senderJIDStr]; ok {
		return name
	}
	return "+" + senderJID.User
}
