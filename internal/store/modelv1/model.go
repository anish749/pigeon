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
	ID          string       `json:"id"`
	Ts          time.Time    `json:"ts"`
	Sender      string       `json:"sender"`
	SenderID    string       `json:"from"`
	Via         Via          `json:"via,omitempty"`
	ReplyTo     string       `json:"replyTo,omitempty"`
	Text        string       `json:"text,omitempty"`
	Reply       bool         `json:"reply,omitempty"`
	Attachments []Attachment `json:"attach,omitempty"`
}

// Attachment references a file stored in the conversation's attachments/ directory.
type Attachment struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// ReactLine represents a reaction or unreaction event.
type ReactLine struct {
	Ts       time.Time `json:"ts"`
	MsgID    string    `json:"msg"`
	Sender   string    `json:"sender"`
	SenderID string    `json:"from"`
	Via      Via       `json:"via,omitempty"`
	Emoji    string    `json:"emoji"`
	Remove   bool      `json:"-"` // derived from LineType, not serialized
}

// EditLine represents a message edit event.
type EditLine struct {
	Ts          time.Time    `json:"ts"`
	MsgID       string       `json:"msg"`
	Sender      string       `json:"sender"`
	SenderID    string       `json:"from"`
	Via         Via          `json:"via,omitempty"`
	Text        string       `json:"text,omitempty"`
	Attachments []Attachment `json:"attach,omitempty"`
}

// DeleteLine represents a message delete event.
type DeleteLine struct {
	Ts       time.Time `json:"ts"`
	MsgID    string    `json:"msg"`
	Sender   string    `json:"sender"`
	SenderID string    `json:"from"`
	Via      Via       `json:"via,omitempty"`
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

// MarshalJSON serialises a Line as a flat JSON object with a "type" field.
func (l Line) MarshalJSON() ([]byte, error) {
	switch l.Type {
	case LineMessage:
		if l.Msg == nil {
			return nil, fmt.Errorf("marshal line: nil Msg for type %s", l.Type)
		}
		return marshalWithType(l.Type, l.Msg)
	case LineReaction, LineUnreaction:
		if l.React == nil {
			return nil, fmt.Errorf("marshal line: nil React for type %s", l.Type)
		}
		return marshalWithType(l.Type, l.React)
	case LineEdit:
		if l.Edit == nil {
			return nil, fmt.Errorf("marshal line: nil Edit for type %s", l.Type)
		}
		return marshalWithType(l.Type, l.Edit)
	case LineDelete:
		if l.Delete == nil {
			return nil, fmt.Errorf("marshal line: nil Delete for type %s", l.Type)
		}
		return marshalWithType(l.Type, l.Delete)
	case LineSeparator:
		return []byte(`{"type":"separator"}`), nil
	default:
		return nil, fmt.Errorf("marshal line: unknown type %q", l.Type)
	}
}

// UnmarshalJSON parses a flat JSON object with a "type" field into a Line.
func (l *Line) UnmarshalJSON(data []byte) error {
	var envelope struct {
		Type LineType `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("unmarshal line type: %w", err)
	}

	l.Type = envelope.Type
	switch envelope.Type {
	case LineMessage:
		l.Msg = &MsgLine{}
		return json.Unmarshal(data, l.Msg)
	case LineReaction:
		l.React = &ReactLine{}
		if err := json.Unmarshal(data, l.React); err != nil {
			return err
		}
		return nil
	case LineUnreaction:
		l.React = &ReactLine{Remove: true}
		if err := json.Unmarshal(data, l.React); err != nil {
			return err
		}
		return nil
	case LineEdit:
		l.Edit = &EditLine{}
		return json.Unmarshal(data, l.Edit)
	case LineDelete:
		l.Delete = &DeleteLine{}
		return json.Unmarshal(data, l.Delete)
	case LineSeparator:
		return nil
	default:
		return fmt.Errorf("unmarshal line: unknown type %q", envelope.Type)
	}
}

// marshalWithType marshals a struct as JSON and injects a "type" field.
func marshalWithType(typ LineType, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Insert "type":"..." right after the opening {
	prefix := fmt.Appendf(nil, `{"type":%q,`, typ)
	// data is `{...}`, we want `{"type":"...", ...}`
	result := make([]byte, 0, len(prefix)+len(data)-1)
	result = append(result, prefix...)
	result = append(result, data[1:]...) // skip the opening {
	return result, nil
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
