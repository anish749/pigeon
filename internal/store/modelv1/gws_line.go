package modelv1

import (
	"encoding/json"
	"fmt"
	"maps"

	calendar "google.golang.org/api/calendar/v3"
	drive "google.golang.org/api/drive/v3"
)

// GWSLine is a parsed JSONL line for a Google Workspace event. Exactly one
// of the typed fields is non-nil. It sits alongside the messaging Line type
// so that GWS events (email, drive comment, calendar event) are first-class
// citizens in the same model package as WhatsApp/Slack messages.
type GWSLine struct {
	Type        string
	Email       *EmailLine
	EmailDelete *EmailDeleteLine
	Comment     *DriveComment
	Event       *CalendarEvent
}

// LineID returns the ID of the line's inner type, used for deduplication.
func (l GWSLine) LineID() string {
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

// gwsTyped is embedded in anonymous structs to inject the "type" field during
// JSON marshaling. The Type field is first so it appears first in the output.
type gwsTyped struct {
	Type string `json:"type"`
}

// MarshalGWS serialises a GWSLine to JSONL (one JSON object, no trailing
// newline). The "type" discriminator is injected from GWSLine.Type — inner
// structs do not carry a Type field.
func MarshalGWS(l GWSLine) ([]byte, error) {
	t := gwsTyped{Type: l.Type}
	switch {
	case l.Email != nil:
		return json.Marshal(struct {
			gwsTyped
			*EmailLine
		}{t, l.Email})
	case l.EmailDelete != nil:
		return json.Marshal(struct {
			gwsTyped
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

// gwsTypeHeader is used to peek at the "type" field before full unmarshal.
type gwsTypeHeader struct {
	Type string `json:"type"`
}

// ParseGWS parses a single JSONL line into a GWSLine.
func ParseGWS(line string) (GWSLine, error) {
	data := []byte(line)

	var hdr gwsTypeHeader
	if err := json.Unmarshal(data, &hdr); err != nil {
		return GWSLine{}, fmt.Errorf("parse line type: %w", err)
	}

	var l GWSLine
	l.Type = hdr.Type

	switch hdr.Type {
	case "email":
		var v EmailLine
		if err := json.Unmarshal(data, &v); err != nil {
			return GWSLine{}, fmt.Errorf("parse email line: %w", err)
		}
		l.Email = &v
	case "email-delete":
		var v EmailDeleteLine
		if err := json.Unmarshal(data, &v); err != nil {
			return GWSLine{}, fmt.Errorf("parse email-delete line: %w", err)
		}
		l.EmailDelete = &v
	case "comment":
		var runtime drive.Comment
		serialized, err := unmarshalRaw(data, &runtime)
		if err != nil {
			return GWSLine{}, fmt.Errorf("parse comment line: %w", err)
		}
		l.Comment = &DriveComment{Runtime: runtime, Serialized: serialized}
	case "event":
		var runtime calendar.Event
		serialized, err := unmarshalRaw(data, &runtime)
		if err != nil {
			return GWSLine{}, fmt.Errorf("parse event line: %w", err)
		}
		l.Event = &CalendarEvent{Runtime: runtime, Serialized: serialized}
	default:
		return GWSLine{}, fmt.Errorf("parse line: unknown type %q", hdr.Type)
	}

	return l, nil
}
