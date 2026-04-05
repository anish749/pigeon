package modelv1

import (
	"encoding/json"
	"fmt"
	"time"
)

// Via represents the message pathway through pigeon.
type Via string

const (
	ViaOrganic      Via = ""               // normal message, user's own connection
	ViaToPigeon     Via = "to-pigeon"      // third party sent this to pigeon's bot
	ViaPigeonAsUser Via = "pigeon-as-user" // pigeon sent using the user's identity
	ViaPigeonAsBot  Via = "pigeon-as-bot"  // pigeon sent using the bot's identity
)

// LineType classifies a parsed line.
type LineType string

const (
	LineMessage    LineType = "msg"
	LineReaction   LineType = "react"
	LineUnreaction LineType = "unreact"
	LineEdit       LineType = "edit"
	LineDelete     LineType = "delete"
	LineSeparator  LineType = "separator"
)

// MsgLine represents a message event.
type MsgLine struct {
	ID          string       `json:"id"`                // platform message ID
	Ts          time.Time    `json:"ts"`                // message timestamp
	Sender      string       `json:"sender"`            // display name (best-effort at write time)
	SenderID    string       `json:"from"`              // platform user ID (stable identity)
	Via         Via          `json:"via,omitempty"`      // message pathway
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
	Ts       time.Time `json:"ts"`               // when the reaction happened
	MsgID    string    `json:"msg"`              // target message ID
	Sender   string    `json:"sender"`           // who reacted (display name)
	SenderID string    `json:"from"`             // who reacted (platform ID)
	Via      Via       `json:"via,omitempty"`     // message pathway
	Emoji    string    `json:"emoji"`            // emoji name or Unicode character
	Remove   bool      `json:"-"`                // true = unreact (derived from LineType, not serialized)
}

// EditLine represents a message edit event.
type EditLine struct {
	Ts          time.Time    `json:"ts"`               // when the edit happened
	MsgID       string       `json:"msg"`              // target message ID
	Sender      string       `json:"sender"`           // who edited (display name)
	SenderID    string       `json:"from"`             // who edited (platform ID)
	Via         Via          `json:"via,omitempty"`     // message pathway
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

// Line is a parsed protocol line. Exactly one of Msg, React, Edit, or
// Delete is non-nil. For LineSeparator, all are nil.
type Line struct {
	Type   LineType
	Msg    *MsgLine
	React  *ReactLine
	Edit   *EditLine
	Delete *DeleteLine
}

// Ts returns the timestamp of the line's inner type.
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
	}
	return time.Time{}
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
	default:
		return nil, fmt.Errorf("marshal line: unknown type %q", l.Type)
	}
	return json.Marshal(v)
}

// Parse parses a single JSONL line into a Line.
func Parse(line string) (Line, error) {
	var t typed
	if err := json.Unmarshal([]byte(line), &t); err != nil {
		return Line{}, fmt.Errorf("parse line: %w", err)
	}

	l := Line{Type: t.Type}
	switch t.Type {
	case LineMessage:
		l.Msg = &MsgLine{}
		if err := json.Unmarshal([]byte(line), l.Msg); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineReaction:
		l.React = &ReactLine{}
		if err := json.Unmarshal([]byte(line), l.React); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineUnreaction:
		l.React = &ReactLine{Remove: true}
		if err := json.Unmarshal([]byte(line), l.React); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineEdit:
		l.Edit = &EditLine{}
		if err := json.Unmarshal([]byte(line), l.Edit); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineDelete:
		l.Delete = &DeleteLine{}
		if err := json.Unmarshal([]byte(line), l.Delete); err != nil {
			return Line{}, fmt.Errorf("parse line: %w", err)
		}
	case LineSeparator:
		// no fields to parse
	case "":
		return Line{}, fmt.Errorf("parse line: missing type field")
	default:
		return Line{}, fmt.Errorf("parse line: unknown type %q", t.Type)
	}
	return l, nil
}

// DateFile holds all parsed events from a single date file.
type DateFile struct {
	Messages  []MsgLine
	Reactions []ReactLine
	Edits     []EditLine
	Deletes   []DeleteLine
}

// ThreadFile holds all parsed events from a single thread file.
type ThreadFile struct {
	Parent    MsgLine
	Replies   []MsgLine
	Context   []MsgLine // channel context messages (before + after parent)
	Reactions []ReactLine
	Edits     []EditLine
	Deletes   []DeleteLine
}
