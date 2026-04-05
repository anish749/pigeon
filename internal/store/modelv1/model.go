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
	ID          string       // platform message ID
	Ts          time.Time    // message timestamp
	Sender      string       // display name (best-effort at write time)
	SenderID    string       // platform user ID (stable identity)
	Via         Via          // message pathway
	ReplyTo     string       // quoted message ID (WhatsApp quote-reply), empty if not a reply
	Text        string       // message body (may contain newlines)
	Reply       bool         // thread reply
	Attachments []Attachment // zero or more attachments
}

// Attachment references a file stored in the conversation's attachments/ directory.
type Attachment struct {
	ID   string `json:"id"`   // platform attachment ID (filename in attachments/)
	Type string `json:"type"` // MIME type (e.g. "image/jpeg")
}

// ReactLine represents a reaction or unreaction event.
type ReactLine struct {
	Ts       time.Time // when the reaction happened
	MsgID    string    // target message ID
	Sender   string    // who reacted (display name)
	SenderID string    // who reacted (platform ID)
	Via      Via       // message pathway
	Emoji    string    // emoji name or Unicode character
	Remove   bool      // true = unreact (derived from LineType, not serialized)
}

// EditLine represents a message edit event.
type EditLine struct {
	Ts          time.Time    // when the edit happened
	MsgID       string       // target message ID
	Sender      string       // who edited (display name)
	SenderID    string       // who edited (platform ID)
	Via         Via          // message pathway
	Text        string       // new message text
	Attachments []Attachment // complete attachment set after edit
}

// DeleteLine represents a message delete event.
type DeleteLine struct {
	Ts       time.Time // when the delete happened
	MsgID    string    // target message ID
	Sender   string    // who deleted (display name)
	SenderID string    // who deleted (platform ID)
	Via      Via       // message pathway
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

// Marshal serialises a Line to JSONL (one JSON object, no trailing newline).
func Marshal(l Line) ([]byte, error) {
	data, err := json.Marshal(toEvent(l))
	if err != nil {
		return nil, fmt.Errorf("marshal line: %w", err)
	}
	return data, nil
}

// Parse parses a single JSONL line into a Line.
func Parse(line string) (Line, error) {
	var e event
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return Line{}, fmt.Errorf("parse line: %w", err)
	}
	switch e.Type {
	case LineMessage, LineReaction, LineUnreaction, LineEdit, LineDelete, LineSeparator:
	case "":
		return Line{}, fmt.Errorf("parse line: missing type field")
	default:
		return Line{}, fmt.Errorf("parse line: unknown type %q", e.Type)
	}
	return fromEvent(e), nil
}

// event is the flat JSON envelope used for serialization. All line types
// share the same struct; unused fields are omitted via omitempty.
// This avoids custom MarshalJSON/UnmarshalJSON — encoding/json handles
// everything natively.
type event struct {
	Type     LineType     `json:"type"`
	ID       string       `json:"id,omitempty"`       // message ID (msg)
	Ts       *time.Time   `json:"ts,omitempty"`        // event timestamp (all except separator)
	Sender   string       `json:"sender,omitempty"`    // display name
	SenderID string       `json:"from,omitempty"`      // platform user ID
	Via      Via          `json:"via,omitempty"`        // message pathway
	Text     string       `json:"text,omitempty"`       // message/edit text
	ReplyTo  string       `json:"replyTo,omitempty"`   // quoted message ID
	Reply    bool         `json:"reply,omitempty"`      // thread reply flag
	Attach   []Attachment `json:"attach,omitempty"`     // attachments
	MsgID    string       `json:"msg,omitempty"`        // target message ID (react/edit/delete)
	Emoji    string       `json:"emoji,omitempty"`      // reaction emoji
}

// toEvent converts a Line to the flat serialization envelope.
func toEvent(l Line) event {
	e := event{Type: l.Type}
	switch l.Type {
	case LineMessage:
		if l.Msg == nil {
			return e
		}
		m := l.Msg
		e.ID = m.ID
		e.Ts = &m.Ts
		e.Sender = m.Sender
		e.SenderID = m.SenderID
		e.Via = m.Via
		e.Text = m.Text
		e.ReplyTo = m.ReplyTo
		e.Reply = m.Reply
		e.Attach = m.Attachments
	case LineReaction, LineUnreaction:
		if l.React == nil {
			return e
		}
		r := l.React
		e.Ts = &r.Ts
		e.MsgID = r.MsgID
		e.Sender = r.Sender
		e.SenderID = r.SenderID
		e.Via = r.Via
		e.Emoji = r.Emoji
	case LineEdit:
		if l.Edit == nil {
			return e
		}
		ed := l.Edit
		e.Ts = &ed.Ts
		e.MsgID = ed.MsgID
		e.Sender = ed.Sender
		e.SenderID = ed.SenderID
		e.Via = ed.Via
		e.Text = ed.Text
		e.Attach = ed.Attachments
	case LineDelete:
		if l.Delete == nil {
			return e
		}
		d := l.Delete
		e.Ts = &d.Ts
		e.MsgID = d.MsgID
		e.Sender = d.Sender
		e.SenderID = d.SenderID
		e.Via = d.Via
	}
	return e
}

// fromEvent converts the flat serialization envelope back to a Line.
func fromEvent(e event) Line {
	l := Line{Type: e.Type}
	switch e.Type {
	case LineMessage:
		m := &MsgLine{
			ID:       e.ID,
			Sender:   e.Sender,
			SenderID: e.SenderID,
			Via:      e.Via,
			Text:     e.Text,
			ReplyTo:  e.ReplyTo,
			Reply:    e.Reply,
		}
		if e.Ts != nil {
			m.Ts = *e.Ts
		}
		m.Attachments = e.Attach
		l.Msg = m
	case LineReaction:
		r := &ReactLine{
			MsgID:    e.MsgID,
			Sender:   e.Sender,
			SenderID: e.SenderID,
			Via:      e.Via,
			Emoji:    e.Emoji,
		}
		if e.Ts != nil {
			r.Ts = *e.Ts
		}
		l.React = r
	case LineUnreaction:
		r := &ReactLine{
			MsgID:    e.MsgID,
			Sender:   e.Sender,
			SenderID: e.SenderID,
			Via:      e.Via,
			Emoji:    e.Emoji,
			Remove:   true,
		}
		if e.Ts != nil {
			r.Ts = *e.Ts
		}
		l.React = r
	case LineEdit:
		ed := &EditLine{
			MsgID:    e.MsgID,
			Sender:   e.Sender,
			SenderID: e.SenderID,
			Via:      e.Via,
			Text:     e.Text,
		}
		if e.Ts != nil {
			ed.Ts = *e.Ts
		}
		ed.Attachments = e.Attach
		l.Edit = ed
	case LineDelete:
		d := &DeleteLine{
			MsgID:    e.MsgID,
			Sender:   e.Sender,
			SenderID: e.SenderID,
			Via:      e.Via,
		}
		if e.Ts != nil {
			d.Ts = *e.Ts
		}
		l.Delete = d
	}
	return l
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
