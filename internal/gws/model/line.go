package model

import (
	"encoding/json"
	"fmt"
)

// Line is a parsed JSONL line. Exactly one of the typed fields is non-nil.
type Line struct {
	Type        string
	Email       *EmailLine
	EmailDelete *EmailDeleteLine
	Comment     *CommentLine
	Reply       *ReplyLine
	Event       *EventLine
}

// LineID returns the ID of the line's inner type, used for deduplication.
func (l Line) LineID() string {
	switch {
	case l.Email != nil:
		return l.Email.ID
	case l.EmailDelete != nil:
		return l.EmailDelete.ID
	case l.Comment != nil:
		return l.Comment.ID
	case l.Reply != nil:
		return l.Reply.ID
	case l.Event != nil:
		return l.Event.ID
	default:
		return ""
	}
}

// typed is embedded in anonymous structs to inject the "type" field during
// JSON marshaling. The Type field is first so it appears first in the output.
type typed struct {
	Type string `json:"type"`
}

// Marshal serialises a Line to JSONL (one JSON object, no trailing newline).
// The "type" discriminator is injected from Line.Type — inner structs do not
// carry a Type field.
func Marshal(l Line) ([]byte, error) {
	t := typed{Type: l.Type}
	switch {
	case l.Email != nil:
		return json.Marshal(struct {
			typed
			*EmailLine
		}{t, l.Email})
	case l.EmailDelete != nil:
		return json.Marshal(struct {
			typed
			*EmailDeleteLine
		}{t, l.EmailDelete})
	case l.Comment != nil:
		return json.Marshal(struct {
			typed
			*CommentLine
		}{t, l.Comment})
	case l.Reply != nil:
		return json.Marshal(struct {
			typed
			*ReplyLine
		}{t, l.Reply})
	case l.Event != nil:
		return json.Marshal(struct {
			typed
			*EventLine
		}{t, l.Event})
	default:
		return nil, fmt.Errorf("marshal line: no typed field set")
	}
}

// typeHeader is used to peek at the "type" field before full unmarshal.
type typeHeader struct {
	Type string `json:"type"`
}

// Parse parses a single JSONL line into a Line.
func Parse(line string) (Line, error) {
	data := []byte(line)

	var hdr typeHeader
	if err := json.Unmarshal(data, &hdr); err != nil {
		return Line{}, fmt.Errorf("parse line type: %w", err)
	}

	var l Line
	l.Type = hdr.Type

	switch hdr.Type {
	case "email":
		var v EmailLine
		if err := json.Unmarshal(data, &v); err != nil {
			return Line{}, fmt.Errorf("parse email line: %w", err)
		}
		l.Email = &v
	case "email-delete":
		var v EmailDeleteLine
		if err := json.Unmarshal(data, &v); err != nil {
			return Line{}, fmt.Errorf("parse email-delete line: %w", err)
		}
		l.EmailDelete = &v
	case "comment":
		var v CommentLine
		if err := json.Unmarshal(data, &v); err != nil {
			return Line{}, fmt.Errorf("parse comment line: %w", err)
		}
		l.Comment = &v
	case "reply":
		var v ReplyLine
		if err := json.Unmarshal(data, &v); err != nil {
			return Line{}, fmt.Errorf("parse reply line: %w", err)
		}
		l.Reply = &v
	case "event":
		var v EventLine
		if err := json.Unmarshal(data, &v); err != nil {
			return Line{}, fmt.Errorf("parse event line: %w", err)
		}
		l.Event = &v
	default:
		return Line{}, fmt.Errorf("parse line: unknown type %q", hdr.Type)
	}

	return l, nil
}
