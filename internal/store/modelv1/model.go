package modelv1

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"

	calendar "google.golang.org/api/calendar/v3"
	drive "google.golang.org/api/drive/v3"
)

// Via represents the message pathway through pigeon.
type Via string

const (
	ViaOrganic      Via = ""               // normal message, user's own connection
	ViaToPigeon     Via = "to-pigeon"      // third party sent this to pigeon's bot
	ViaPigeonAsUser Via = "pigeon-as-user" // pigeon sent using the user's identity
	ViaPigeonAsBot  Via = "pigeon-as-bot"  // pigeon sent using the bot's identity
)

// LineType classifies a parsed line. Messaging types (msg, react, etc.) and
// Google Workspace types (email, comment, etc.) share the same discriminator
// space because they all live in the same JSONL format on disk.
type LineType string

const (
	LineMessage    LineType = "msg"
	LineReaction   LineType = "react"
	LineUnreaction LineType = "unreact"
	LineEdit       LineType = "edit"
	LineDelete     LineType = "delete"
	LineSeparator  LineType = "separator"
	LineEmail      LineType = "email"
	LineComment    LineType = "comment"
	LineEvent      LineType = "event"
)

// MsgLine represents a message event.
//
// The "ts" field name and ISO 8601 format are depended on by read.threadDatePatterns,
// which uses rg -l to find thread files by matching "ts":"YYYY-MM-DD in serialized JSONL.
type MsgLine struct {
	ID          string       `json:"id"`                // platform message ID
	Ts          time.Time    `json:"ts"`                // message timestamp
	Sender      string       `json:"sender"`            // display name (best-effort at write time)
	SenderID    string       `json:"from"`              // platform user ID (stable identity)
	Via         Via          `json:"via,omitempty"`     // message pathway
	ReplyTo     string       `json:"replyTo,omitempty"` // quoted message ID (WhatsApp quote-reply), empty if not a reply
	Text        string       `json:"text,omitempty"`    // message body (may contain newlines)
	Reply       bool         `json:"reply,omitempty"`   // thread reply
	Attachments []Attachment `json:"attach,omitempty"`  // zero or more attachments
}

// Attachment references a file stored in the conversation's attachments/ directory.
type Attachment struct {
	ID   string `json:"id"`   // platform attachment ID (filename in attachments/)
	Type string `json:"type"` // MIME type (e.g. "image/jpeg")
}

// ReactLine represents a reaction or unreaction event.
type ReactLine struct {
	Ts       time.Time `json:"ts"`            // when the reaction happened
	MsgID    string    `json:"msg"`           // target message ID
	Sender   string    `json:"sender"`        // who reacted (display name)
	SenderID string    `json:"from"`          // who reacted (platform ID)
	Via      Via       `json:"via,omitempty"` // message pathway
	Emoji    string    `json:"emoji"`         // emoji name or Unicode character
	Remove   bool      `json:"-"`             // true = unreact (derived from LineType, not serialized)
}

// EditLine represents a message edit event.
type EditLine struct {
	Ts          time.Time    `json:"ts"`               // when the edit happened
	MsgID       string       `json:"msg"`              // target message ID
	Sender      string       `json:"sender"`           // who edited (display name)
	SenderID    string       `json:"from"`             // who edited (platform ID)
	Via         Via          `json:"via,omitempty"`    // message pathway
	Text        string       `json:"text,omitempty"`   // new message text
	Attachments []Attachment `json:"attach,omitempty"` // complete attachment set after edit
}

// DeleteLine represents a message delete event.
type DeleteLine struct {
	Ts       time.Time `json:"ts"`            // when the delete happened
	MsgID    string    `json:"msg"`           // target message ID
	Sender   string    `json:"sender"`        // who deleted (display name)
	SenderID string    `json:"from"`          // who deleted (platform ID)
	Via      Via       `json:"via,omitempty"` // message pathway
}

// Line is a parsed protocol line. Exactly one of the payload pointers is
// non-nil (none for LineSeparator). Messaging payloads (Msg, React, Edit,
// Delete) and Google Workspace payloads (Email, Comment, Event) share one
// envelope because they use the same JSONL format on disk.
type Line struct {
	Type    LineType
	Msg     *MsgLine
	React   *ReactLine
	Edit    *EditLine
	Delete  *DeleteLine
	Email   *EmailLine
	Comment *DriveComment
	Event   *CalendarEvent
}

// Ts returns the timestamp of the line's inner type. Returns the zero time
// for LineComment and LineEvent, whose timestamps live in nested API
// structures rather than a single field — callers that need a storage date
// for those types should derive it explicitly (e.g. CalendarEvent.DateForStorage).
func (l Line) Ts() time.Time {
	switch l.Type {
	case LineMessage:
		if l.Msg != nil {
			return l.Msg.Ts
		}
	case LineReaction, LineUnreaction:
		if l.React != nil {
			return l.React.Ts
		}
	case LineEdit:
		if l.Edit != nil {
			return l.Edit.Ts
		}
	case LineDelete:
		if l.Delete != nil {
			return l.Delete.Ts
		}
	case LineEmail:
		if l.Email != nil {
			return l.Email.Ts
		}
	}
	return time.Time{}
}

// ID returns the identifier of the line's inner type, used for deduplication.
// The second return value is false for types that don't carry a standalone ID
// (reactions, edits, deletes, separators).
func (l Line) ID() (string, bool) {
	switch l.Type {
	case LineMessage:
		if l.Msg != nil {
			return l.Msg.ID, true
		}
	case LineEmail:
		if l.Email != nil {
			return l.Email.ID, true
		}
	case LineComment:
		if l.Comment != nil {
			return l.Comment.Runtime.Id, true
		}
	case LineEvent:
		if l.Event != nil {
			return l.Event.Runtime.Id, true
		}
	}
	return "", false
}

// SeparatorLine is the JSON representation of a separator event.
const SeparatorLine = `{"type":"separator"}`

// typed is embedded in anonymous structs to inject the "type" JSON field.
type typed struct {
	Type LineType `json:"type"`
}

// Marshal serialises a Line to JSONL (one JSON object, no trailing newline).
func Marshal(l Line) ([]byte, error) {
	var v any
	switch l.Type {
	case LineMessage:
		v = struct {
			typed
			*MsgLine
		}{typed{l.Type}, l.Msg}
	case LineReaction, LineUnreaction:
		v = struct {
			typed
			*ReactLine
		}{typed{l.Type}, l.React}
	case LineEdit:
		v = struct {
			typed
			*EditLine
		}{typed{l.Type}, l.Edit}
	case LineDelete:
		v = struct {
			typed
			*DeleteLine
		}{typed{l.Type}, l.Delete}
	case LineSeparator:
		return []byte(SeparatorLine), nil
	case LineEmail:
		v = struct {
			typed
			*EmailLine
		}{typed{l.Type}, l.Email}
	case LineComment:
		return marshalRaw(l.Comment.Serialized, string(LineComment))
	case LineEvent:
		return marshalRaw(l.Event.Serialized, string(LineEvent))
	default:
		return nil, fmt.Errorf("marshal line: unknown type %q", l.Type)
	}
	return json.Marshal(v)
}

// Parse parses a single JSONL line into a Line.
func Parse(line string) (Line, error) {
	data := []byte(line)

	var t typed
	if err := json.Unmarshal(data, &t); err != nil {
		return Line{}, fmt.Errorf("parse line: %w", err)
	}

	l := Line{Type: t.Type}
	switch t.Type {
	case LineMessage:
		l.Msg = &MsgLine{}
		if err := json.Unmarshal(data, l.Msg); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineReaction:
		l.React = &ReactLine{}
		if err := json.Unmarshal(data, l.React); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineUnreaction:
		l.React = &ReactLine{Remove: true}
		if err := json.Unmarshal(data, l.React); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineEdit:
		l.Edit = &EditLine{}
		if err := json.Unmarshal(data, l.Edit); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineDelete:
		l.Delete = &DeleteLine{}
		if err := json.Unmarshal(data, l.Delete); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineSeparator:
		// no fields to parse
	case LineEmail:
		l.Email = &EmailLine{}
		if err := json.Unmarshal(data, l.Email); err != nil {
			return Line{}, fmt.Errorf("parse email line: %w", err)
		}
	case LineComment:
		var runtime drive.Comment
		serialized, err := unmarshalRaw(data, &runtime)
		if err != nil {
			return Line{}, fmt.Errorf("parse comment line: %w", err)
		}
		l.Comment = &DriveComment{Runtime: runtime, Serialized: serialized}
	case LineEvent:
		var runtime calendar.Event
		serialized, err := unmarshalRaw(data, &runtime)
		if err != nil {
			return Line{}, fmt.Errorf("parse event line: %w", err)
		}
		l.Event = &CalendarEvent{Runtime: runtime, Serialized: serialized}
	case "":
		return Line{}, fmt.Errorf("parse line: missing type field")
	default:
		return Line{}, fmt.Errorf("parse line: unknown type %q", t.Type)
	}
	return l, nil
}

// marshalRaw copies a serialized map and injects the storage type discriminator
// without mutating the caller's map. Used by LineComment and LineEvent, which
// persist the raw API map verbatim so that fields the typed SDK structs don't
// know about still round-trip through disk.
func marshalRaw(serialized map[string]any, typeTag string) ([]byte, error) {
	out := make(map[string]any, len(serialized)+1)
	maps.Copy(out, serialized)
	out["type"] = typeTag
	return json.Marshal(out)
}

// unmarshalRaw unmarshals the same bytes into both a typed destination and a
// raw map, then strips the storage type discriminator from the map. It is
// the symmetric counterpart to marshalRaw: marshal builds a map-with-type
// from a Serialized field; unmarshal rebuilds Serialized without the type.
func unmarshalRaw(data []byte, runtime any) (map[string]any, error) {
	if err := json.Unmarshal(data, runtime); err != nil {
		return nil, err
	}
	var serialized map[string]any
	if err := json.Unmarshal(data, &serialized); err != nil {
		return nil, err
	}
	// "type" is our storage discriminator, not part of the API response.
	delete(serialized, "type")
	return serialized, nil
}

// DateFile holds all parsed events from a single date file.
type DateFile struct {
	Messages  []MsgLine
	Reactions []ReactLine
	Edits     []EditLine
	Deletes   []DeleteLine
}

// ThreadFile holds all parsed events from a single thread file.
// Context messages are stored so that grep and search commands that read
// thread files directly can surface surrounding channel messages.
type ThreadFile struct {
	Parent    MsgLine
	Replies   []MsgLine
	Context   []MsgLine // channel messages around the parent, searchable via grep/rg
	Reactions []ReactLine
	Edits     []EditLine
	Deletes   []DeleteLine
}
