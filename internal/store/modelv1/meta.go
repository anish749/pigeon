package modelv1

// ConversationType is the type of conversation stored in .meta.json.
type ConversationType string

const (
	ConvChannel ConversationType = "channel"  // Slack public or private channel
	ConvDM      ConversationType = "dm"       // 1:1 direct message (Slack or WhatsApp)
	ConvGroupDM ConversationType = "group_dm" // Slack multi-party DM (MPDM)
	ConvGroup   ConversationType = "group"    // WhatsApp group
)

// ConversationMeta is the per-conversation .meta.json content.
// See docs/protocol.md § Conversation Metadata.
//
// Use the constructors (NewSlackChannelMeta, NewSlackDMMeta, etc.) instead
// of building the struct directly — they enforce required fields.
type ConversationMeta struct {
	Name      string           `json:"name"`
	Type      ConversationType `json:"type"`
	ChannelID string           `json:"channel_id,omitempty"`
	UserID    string           `json:"user_id,omitempty"`
	JID       string           `json:"jid,omitempty"`
}

// Slack constructors.

// NewSlackChannelMeta creates metadata for a Slack public or private channel.
func NewSlackChannelMeta(name, channelID string) ConversationMeta {
	return ConversationMeta{
		Name:      name,
		Type:      ConvChannel,
		ChannelID: channelID,
	}
}

// NewSlackDMMeta creates metadata for a Slack 1:1 DM.
func NewSlackDMMeta(name, channelID, userID string) ConversationMeta {
	return ConversationMeta{
		Name:      name,
		Type:      ConvDM,
		ChannelID: channelID,
		UserID:    userID,
	}
}

// NewSlackGroupDMMeta creates metadata for a Slack multi-party DM (MPDM).
func NewSlackGroupDMMeta(name, channelID string) ConversationMeta {
	return ConversationMeta{
		Name:      name,
		Type:      ConvGroupDM,
		ChannelID: channelID,
	}
}

// WhatsApp constructors.

// NewWhatsAppDMMeta creates metadata for a WhatsApp 1:1 conversation.
func NewWhatsAppDMMeta(name, jid string) ConversationMeta {
	return ConversationMeta{
		Name: name,
		Type: ConvDM,
		JID:  jid,
	}
}

// NewWhatsAppGroupMeta creates metadata for a WhatsApp group conversation.
func NewWhatsAppGroupMeta(name, jid string) ConversationMeta {
	return ConversationMeta{
		Name: name,
		Type: ConvGroup,
		JID:  jid,
	}
}
