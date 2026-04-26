package whatsapp

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ExtractText returns the text content from a WhatsApp message, checking multiple
// message types in order of likelihood. For media messages with captions, the
// caption is returned. Media-only messages return empty string.
func ExtractText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	if msg.ImageMessage != nil && msg.ImageMessage.Caption != nil {
		return *msg.ImageMessage.Caption
	}
	if msg.VideoMessage != nil && msg.VideoMessage.Caption != nil {
		return *msg.VideoMessage.Caption
	}
	if msg.DocumentMessage != nil && msg.DocumentMessage.Caption != nil {
		return *msg.DocumentMessage.Caption
	}
	return ""
}

// SanitizeFilename replaces characters that are unsafe in filesystem paths.
func SanitizeFilename(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "\x00", "")
	return replacer.Replace(name)
}

// EditedMessage extracts the original message ID and the new message contents
// from a live edit event. WhatsApp delivers edits as a Message wrapped in
// EditedMessage; whatsmeow's UnwrapRaw peels off the wrapper and sets
// Message.IsEdit, but leaves the inner ProtocolMessage intact. The original
// (target) message ID lives in ProtocolMessage.Key.ID, and the replacement
// content lives in ProtocolMessage.EditedMessage.
//
// Returns ("", nil) when msg is not a MESSAGE_EDIT protocol message.
func EditedMessage(msg *waE2E.Message) (origID string, edited *waE2E.Message) {
	proto := msg.GetProtocolMessage()
	if proto.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT {
		return "", nil
	}
	return proto.GetKey().GetID(), proto.GetEditedMessage()
}

// RevokedMessageID returns the ID of a message being revoked (deleted), or
// the empty string if msg is not a REVOKE protocol message. REVOKE is the
// default value of ProtocolMessage_Type, so callers must guard against bare
// non-protocol messages — this helper checks ProtocolMessage is set first.
func RevokedMessageID(msg *waE2E.Message) string {
	proto := msg.GetProtocolMessage()
	if proto == nil || proto.GetType() != waE2E.ProtocolMessage_REVOKE {
		return ""
	}
	return proto.GetKey().GetID()
}
