package outbox

import (
	"encoding/json"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// payloadHeader holds the fields parsed from the item payload that are
// needed for domain logic. Kept minimal to avoid coupling outbox to the
// api package (which imports outbox).
type payloadHeader struct {
	Platform string      `json:"platform"`
	Via      modelv1.Via `json:"via"`
}

func (it *Item) header() payloadHeader {
	var h payloadHeader
	json.Unmarshal(it.Payload, &h)
	return h
}

// Via returns the current send mode from the item payload.
func (it *Item) Via() modelv1.Via {
	return it.header().Via
}

// Platform returns the target platform from the item payload.
func (it *Item) Platform() string {
	return it.header().Platform
}

// HasSession reports whether the item has an originating session that can
// receive feedback.
func (it *Item) HasSession() bool {
	return it.SessionID != ""
}

// CycleVia returns the next send mode in the rotation:
//
//	"" (bot) → "pigeon-as-user" → "pigeon-as-bot" → "" → ...
//
// WhatsApp items always return empty (user sends as themselves).
func (it *Item) CycleVia() modelv1.Via {
	h := it.header()
	if h.Platform == "whatsapp" {
		return ""
	}
	switch h.Via {
	case "", modelv1.ViaPigeonAsBot:
		return modelv1.ViaPigeonAsUser
	default:
		return modelv1.ViaPigeonAsBot
	}
}
