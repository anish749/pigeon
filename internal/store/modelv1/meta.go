package modelv1

// ConversationMeta is the per-conversation .meta.json content.
// See docs/protocol.md § Conversation Metadata.
type ConversationMeta struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	ChannelID string `json:"channel_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	JID       string `json:"jid,omitempty"`
}
