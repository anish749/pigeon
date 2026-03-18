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
