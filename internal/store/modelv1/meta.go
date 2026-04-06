package modelv1

// ConvType classifies a conversation.
type ConvType string

const (
	ConvChannel ConvType = "channel"  // Slack public or private channel
	ConvDM      ConvType = "dm"       // 1:1 direct message (Slack or WhatsApp)
	ConvGroupDM ConvType = "group_dm" // Slack multi-party direct message (MPDM)
	ConvGroup   ConvType = "group"    // WhatsApp group
)

// ConvMeta is the .meta.json sidecar for a conversation directory.
// It maps the directory's display name back to stable platform IDs.
type ConvMeta struct {
	Name      string   `json:"name"`                 // display name as shown in pigeon list
	Type      ConvType `json:"type"`                 // conversation type
	ChannelID string   `json:"channel_id,omitempty"` // Slack channel ID (C/D/G prefixed)
	UserID    string   `json:"user_id,omitempty"`    // Slack DM partner's user ID (U prefixed)
	JID       string   `json:"jid,omitempty"`        // WhatsApp JID (phone-based, e.g. 14155551234@s.whatsapp.net)
	LID       string   `json:"lid,omitempty"`        // WhatsApp LID (opaque, e.g. abc123@lid)
}

// NewSlackChannel creates metadata for a Slack channel (public or private).
func NewSlackChannel(name, channelID string) ConvMeta {
	return ConvMeta{Name: name, Type: ConvChannel, ChannelID: channelID}
}

// NewSlackDM creates metadata for a Slack 1:1 DM.
func NewSlackDM(name, channelID, userID string) ConvMeta {
	return ConvMeta{Name: name, Type: ConvDM, ChannelID: channelID, UserID: userID}
}

// NewSlackGroupDM creates metadata for a Slack multi-party DM.
func NewSlackGroupDM(name, channelID string) ConvMeta {
	return ConvMeta{Name: name, Type: ConvGroupDM, ChannelID: channelID}
}

// NewWhatsAppDM creates metadata for a WhatsApp 1:1 conversation.
// jid is the phone-based JID, lid is the opaque linked ID. At least one must be non-empty.
func NewWhatsAppDM(name, jid, lid string) ConvMeta {
	return ConvMeta{Name: name, Type: ConvDM, JID: jid, LID: lid}
}

// NewWhatsAppGroup creates metadata for a WhatsApp group conversation.
func NewWhatsAppGroup(name, jid string) ConvMeta {
	return ConvMeta{Name: name, Type: ConvGroup, JID: jid}
}
