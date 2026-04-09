package model

import (
	"encoding/json"
	"fmt"
	"maps"

	calendar "google.golang.org/api/calendar/v3"
	drive "google.golang.org/api/drive/v3"
)

// Line is a parsed JSONL line. Exactly one of the typed fields is non-nil.
type Line struct {
	Type        string
	Email       *EmailLine
	EmailDelete *EmailDeleteLine
	Comment     *DriveComment
	Event       *CalendarEvent
}

// LineID returns the ID of the line's inner type, used for deduplication.
func (l Line) LineID() string {
	switch {
	case l.Email != nil:
		return l.Email.ID
	case l.EmailDelete != nil:
		return l.EmailDelete.ID
	case l.Comment != nil:
		return l.Comment.Runtime.Id
	case l.Event != nil:
		return l.Event.Runtime.Id
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
		return marshalRaw(l.Comment.Serialized, "comment")
	case l.Event != nil:
		return marshalRaw(l.Event.Serialized, "event")
	default:
		return nil, fmt.Errorf("marshal line: no typed field set")
	}
}

// marshalRaw copies a serialized map and injects the storage type discriminator
// without mutating the caller's map.
func marshalRaw(serialized map[string]any, typeTag string) ([]byte, error) {
	out := make(map[string]any, len(serialized)+1)
	maps.Copy(out, serialized)
	out["type"] = typeTag
	return json.Marshal(out)
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
		var runtime drive.Comment
		if err := json.Unmarshal(data, &runtime); err != nil {
			return Line{}, fmt.Errorf("parse comment line: %w", err)
		}
		var serialized map[string]any
		if err := json.Unmarshal(data, &serialized); err != nil {
			return Line{}, fmt.Errorf("parse comment line serialized: %w", err)
		}
		// "type" is our storage discriminator, not part of the API response.
		delete(serialized, "type")
		l.Comment = &DriveComment{Runtime: runtime, Serialized: serialized}
	case "event":
		var runtime calendar.Event
		if err := json.Unmarshal(data, &runtime); err != nil {
			return Line{}, fmt.Errorf("parse event line: %w", err)
		}
		var serialized map[string]any
		if err := json.Unmarshal(data, &serialized); err != nil {
			return Line{}, fmt.Errorf("parse event line serialized: %w", err)
		}
		// "type" is our storage discriminator, not part of the API response.
		delete(serialized, "type")
		l.Event = &CalendarEvent{Runtime: runtime, Serialized: serialized}
	default:
		return Line{}, fmt.Errorf("parse line: unknown type %q", hdr.Type)
	}

	return l, nil
}
