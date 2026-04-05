package modelv1

import (
	"fmt"
	"strings"
	"time"
)

const (
	// tsLayout is the Go time format for protocol timestamps.
	tsLayout = "2006-01-02 15:04:05 -07:00"

	// SeparatorLine is the literal separator used in thread files.
	SeparatorLine = "--- channel context ---"

	// indentPrefix is the 2-space prefix for thread replies.
	indentPrefix = "  "
)

// Marshal serialises a Line to its protocol representation.
// Returns an error if the line type is unrecognized or the required pointer is nil.
func Marshal(l Line) (string, error) {
	switch l.Type {
	case LineMessage:
		if l.Msg == nil {
			return "", fmt.Errorf("marshal: LineMessage with nil Msg")
		}
		return marshalMsg(*l.Msg), nil
	case LineReaction:
		if l.React == nil {
			return "", fmt.Errorf("marshal: LineReaction with nil React")
		}
		return marshalReaction(*l.React, "react"), nil
	case LineUnreaction:
		if l.React == nil {
			return "", fmt.Errorf("marshal: LineUnreaction with nil React")
		}
		return marshalReaction(*l.React, "unreact"), nil
	case LineEdit:
		if l.Edit == nil {
			return "", fmt.Errorf("marshal: LineEdit with nil Edit")
		}
		return marshalEdit(*l.Edit), nil
	case LineDelete:
		if l.Delete == nil {
			return "", fmt.Errorf("marshal: LineDelete with nil Delete")
		}
		return marshalDelete(*l.Delete), nil
	case LineSeparator:
		return SeparatorLine, nil
	default:
		return "", fmt.Errorf("marshal: unknown line type %d", l.Type)
	}
}

// Parse parses a single protocol line into a Line.
func Parse(line string) (Line, error) {
	if line == SeparatorLine {
		return Line{Type: LineSeparator}, nil
	}

	rest := line
	var reply bool

	// 1. Strip optional 2-space indent (thread reply).
	if strings.HasPrefix(rest, indentPrefix) {
		reply = true
		rest = rest[len(indentPrefix):]
	}

	// 2. Parse timestamp (first bracket group, 28 chars including brackets).
	if len(rest) < 28 || rest[0] != '[' {
		return Line{}, fmt.Errorf("parse line: missing timestamp: %q", truncate(line, 80))
	}
	closeBracket := strings.IndexByte(rest, ']')
	if closeBracket < 0 {
		return Line{}, fmt.Errorf("parse line: unclosed timestamp bracket: %q", truncate(line, 80))
	}
	tsStr := rest[1:closeBracket]
	ts, err := time.Parse(tsLayout, tsStr)
	if err != nil {
		return Line{}, fmt.Errorf("parse line: bad timestamp %q: %w", tsStr, err)
	}
	rest = rest[closeBracket+1:]

	// 3. Consume bracket tags.
	type tag struct {
		key string
		val string
	}
	var tags []tag

	for {
		rest = strings.TrimLeft(rest, " ")
		if len(rest) == 0 || rest[0] != '[' {
			break
		}
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return Line{}, fmt.Errorf("parse line: unclosed bracket tag: %q", truncate(line, 80))
		}
		inner := rest[1:end]
		colonIdx := strings.IndexByte(inner, ':')
		if colonIdx < 0 {
			return Line{}, fmt.Errorf("parse line: bracket tag missing colon: %q", inner)
		}
		tags = append(tags, tag{key: inner[:colonIdx], val: inner[colonIdx+1:]})
		rest = rest[end+1:]
	}

	// 4-5. Sender name up to first ':', content after.
	rest = strings.TrimLeft(rest, " ")
	colonIdx := strings.IndexByte(rest, ':')
	if colonIdx < 0 {
		return Line{}, fmt.Errorf("parse line: no sender/content delimiter ':': %q", truncate(line, 80))
	}
	sender := rest[:colonIdx]
	content := strings.TrimPrefix(rest[colonIdx+1:], " ")

	// Extract common tag values.
	var senderID string
	var via Via
	for _, t := range tags {
		switch t.key {
		case "from":
			senderID = t.val
		case "via":
			via = Via(t.val)
		}
	}

	// 6. Classify by distinguishing tag.
	for _, t := range tags {
		switch t.key {
		case "id":
			m := MsgLine{
				ID:       t.val,
				Ts:       ts,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Text:     unescapeText(content),
				Reply:    reply,
			}
			for _, tt := range tags {
				switch tt.key {
				case "attach":
					m.Attachments = append(m.Attachments, parseAttachValue(tt.val))
				case "reply":
					m.ReplyTo = tt.val
				}
			}
			return Line{Type: LineMessage, Msg: &m}, nil

		case "react":
			r := ReactLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Emoji:    content,
			}
			return Line{Type: LineReaction, React: &r}, nil

		case "unreact":
			r := ReactLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Emoji:    content,
				Remove:   true,
			}
			return Line{Type: LineUnreaction, React: &r}, nil

		case "edit":
			e := EditLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Text:     unescapeText(content),
			}
			for _, tt := range tags {
				if tt.key == "attach" {
					e.Attachments = append(e.Attachments, parseAttachValue(tt.val))
				}
			}
			return Line{Type: LineEdit, Edit: &e}, nil

		case "delete":
			d := DeleteLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
			}
			return Line{Type: LineDelete, Delete: &d}, nil
		}
	}

	return Line{}, fmt.Errorf("parse line: no distinguishing tag (id/react/unreact/edit/delete): %q", truncate(line, 80))
}

// --- internal helpers ---

func escapeText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func unescapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
			case '\\':
				b.WriteByte('\\')
				i++
			default:
				b.WriteByte('\\')
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func sanitizeSender(name string) string {
	return strings.ReplaceAll(name, ":", "")
}

func formatTS(t time.Time) string {
	return "[" + t.UTC().Format(tsLayout) + "]"
}

func marshalMsg(m MsgLine) string {
	var b strings.Builder
	if m.Reply {
		b.WriteString(indentPrefix)
	}
	b.WriteString(formatTS(m.Ts))
	b.WriteString(" [id:")
	b.WriteString(m.ID)
	b.WriteByte(']')
	b.WriteString(" [from:")
	b.WriteString(m.SenderID)
	b.WriteByte(']')
	if m.Via != ViaOrganic {
		b.WriteString(" [via:")
		b.WriteString(string(m.Via))
		b.WriteByte(']')
	}
	for _, a := range m.Attachments {
		b.WriteString(" [attach:")
		b.WriteString(a.ID)
		b.WriteString(" type=")
		b.WriteString(a.Type)
		b.WriteByte(']')
	}
	if m.ReplyTo != "" {
		b.WriteString(" [reply:")
		b.WriteString(m.ReplyTo)
		b.WriteByte(']')
	}
	b.WriteByte(' ')
	b.WriteString(sanitizeSender(m.Sender))
	b.WriteByte(':')
	if m.Text != "" {
		b.WriteByte(' ')
		b.WriteString(escapeText(m.Text))
	}
	return b.String()
}

func marshalReaction(r ReactLine, tag string) string {
	var b strings.Builder
	b.WriteString(formatTS(r.Ts))
	b.WriteString(" [")
	b.WriteString(tag)
	b.WriteByte(':')
	b.WriteString(r.MsgID)
	b.WriteByte(']')
	b.WriteString(" [from:")
	b.WriteString(r.SenderID)
	b.WriteByte(']')
	if r.Via != ViaOrganic {
		b.WriteString(" [via:")
		b.WriteString(string(r.Via))
		b.WriteByte(']')
	}
	b.WriteByte(' ')
	b.WriteString(sanitizeSender(r.Sender))
	b.WriteString(": ")
	b.WriteString(r.Emoji)
	return b.String()
}

func marshalEdit(e EditLine) string {
	var b strings.Builder
	b.WriteString(formatTS(e.Ts))
	b.WriteString(" [edit:")
	b.WriteString(e.MsgID)
	b.WriteByte(']')
	b.WriteString(" [from:")
	b.WriteString(e.SenderID)
	b.WriteByte(']')
	if e.Via != ViaOrganic {
		b.WriteString(" [via:")
		b.WriteString(string(e.Via))
		b.WriteByte(']')
	}
	for _, a := range e.Attachments {
		b.WriteString(" [attach:")
		b.WriteString(a.ID)
		b.WriteString(" type=")
		b.WriteString(a.Type)
		b.WriteByte(']')
	}
	b.WriteByte(' ')
	b.WriteString(sanitizeSender(e.Sender))
	b.WriteByte(':')
	if e.Text != "" {
		b.WriteByte(' ')
		b.WriteString(escapeText(e.Text))
	}
	return b.String()
}

func marshalDelete(d DeleteLine) string {
	var b strings.Builder
	b.WriteString(formatTS(d.Ts))
	b.WriteString(" [delete:")
	b.WriteString(d.MsgID)
	b.WriteByte(']')
	b.WriteString(" [from:")
	b.WriteString(d.SenderID)
	b.WriteByte(']')
	if d.Via != ViaOrganic {
		b.WriteString(" [via:")
		b.WriteString(string(d.Via))
		b.WriteByte(']')
	}
	b.WriteByte(' ')
	b.WriteString(sanitizeSender(d.Sender))
	b.WriteByte(':')
	return b.String()
}

func parseAttachValue(val string) Attachment {
	parts := strings.SplitN(val, " ", 2)
	a := Attachment{ID: parts[0]}
	if len(parts) > 1 {
		for _, attr := range strings.Fields(parts[1]) {
			if strings.HasPrefix(attr, "type=") {
				a.Type = attr[len("type="):]
			}
		}
	}
	return a
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
