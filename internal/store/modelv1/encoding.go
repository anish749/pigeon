package modelv1

import (
	"fmt"
	"strings"
	"time"
)

const (
	// tsLayout is the Go time format for protocol timestamps.
	tsLayout = "2006-01-02 15:04:05 -07:00"

	// separatorLine is the literal separator used in thread files.
	separatorLine = "--- channel context ---"

	// indentPrefix is the 2-space prefix for thread replies.
	indentPrefix = "  "
)

// EscapeText encodes newlines and backslashes in message text so that
// one message always occupies exactly one line on disk.
//
//	\  -> \\
//	\n -> literal \n
func EscapeText(s string) string {
	// Order matters: escape backslashes first, then newlines.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// UnescapeText reverses EscapeText, restoring the original message text.
//
//	\\       -> \
//	literal \n -> \n
func UnescapeText(s string) string {
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
				// Unrecognised escape: keep literal backslash.
				b.WriteByte('\\')
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// SanitizeSender strips colon characters from a display name.
// The protocol uses the first ':' after bracket tags as the sender/content
// delimiter, so colons in names would break parsing.
func SanitizeSender(name string) string {
	return strings.ReplaceAll(name, ":", "")
}

// formatTS formats a time as a bracketed protocol timestamp in UTC.
func formatTS(t time.Time) string {
	return "[" + t.UTC().Format(tsLayout) + "]"
}

// MarshalMsg serialises a message line to its protocol representation.
func MarshalMsg(m MsgLine) string {
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
	b.WriteString(SanitizeSender(m.Sender))
	b.WriteByte(':')
	if m.Text != "" {
		b.WriteByte(' ')
		b.WriteString(EscapeText(m.Text))
	}

	return b.String()
}

// marshalReaction is the shared implementation for react/unreact lines.
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
	b.WriteString(SanitizeSender(r.Sender))
	b.WriteString(": ")
	b.WriteString(r.Emoji)

	return b.String()
}

// MarshalReact serialises a reaction line.
func MarshalReact(r ReactLine) string {
	return marshalReaction(r, "react")
}

// MarshalUnreact serialises an unreaction line.
func MarshalUnreact(r ReactLine) string {
	return marshalReaction(r, "unreact")
}

// MarshalEdit serialises an edit line.
func MarshalEdit(e EditLine) string {
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

	b.WriteByte(' ')
	b.WriteString(SanitizeSender(e.Sender))
	b.WriteByte(':')
	if e.Text != "" {
		b.WriteByte(' ')
		b.WriteString(EscapeText(e.Text))
	}

	return b.String()
}

// MarshalDelete serialises a delete line.
func MarshalDelete(d DeleteLine) string {
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
	b.WriteString(SanitizeSender(d.Sender))
	b.WriteByte(':')

	return b.String()
}

// ParseLine parses a single protocol line and returns its type and the
// corresponding struct. The returned value is one of: MsgLine,
// ReactLine, EditLine, DeleteLine, or nil for LineSeparator.
func ParseLine(line string) (LineType, any, error) {
	// Check for separator first.
	if line == separatorLine {
		return LineSeparator, nil, nil
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
		return 0, nil, fmt.Errorf("parse line: missing timestamp: %q", truncate(line, 80))
	}
	closeBracket := strings.IndexByte(rest, ']')
	if closeBracket < 0 {
		return 0, nil, fmt.Errorf("parse line: unclosed timestamp bracket: %q", truncate(line, 80))
	}
	tsStr := rest[1:closeBracket]
	ts, err := time.Parse(tsLayout, tsStr)
	if err != nil {
		return 0, nil, fmt.Errorf("parse line: bad timestamp %q: %w", tsStr, err)
	}
	rest = rest[closeBracket+1:]

	// 3. Consume bracket tags.
	type tag struct {
		key string // e.g. "id", "from", "attach"
		val string // full value including any key=val attributes
	}
	var tags []tag

	for {
		rest = strings.TrimLeft(rest, " ")
		if len(rest) == 0 || rest[0] != '[' {
			break
		}
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return 0, nil, fmt.Errorf("parse line: unclosed bracket tag: %q", truncate(line, 80))
		}
		inner := rest[1:end] // e.g. "id:MSG_ID" or "attach:F1 type=image/jpeg"
		colonIdx := strings.IndexByte(inner, ':')
		if colonIdx < 0 {
			return 0, nil, fmt.Errorf("parse line: bracket tag missing colon: %q", inner)
		}
		tags = append(tags, tag{key: inner[:colonIdx], val: inner[colonIdx+1:]})
		rest = rest[end+1:]
	}

	// 4–5. Sender name: everything up to first ':'. Content: everything after.
	rest = strings.TrimLeft(rest, " ")
	colonIdx := strings.IndexByte(rest, ':')
	if colonIdx < 0 {
		return 0, nil, fmt.Errorf("parse line: no sender/content delimiter ':': %q", truncate(line, 80))
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
			// Message line.
			m := MsgLine{
				ID:       t.val,
				Ts:       ts,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Text:     UnescapeText(content),
				Reply:    reply,
			}
			// Collect attach and reply tags.
			for _, tt := range tags {
				switch tt.key {
				case "attach":
					a := parseAttachValue(tt.val)
					m.Attachments = append(m.Attachments, a)
				case "reply":
					m.ReplyTo = tt.val
				}
			}
			return LineMessage, m, nil

		case "react":
			r := ReactLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Emoji:    content,
				Remove:   false,
			}
			return LineReaction, r, nil

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
			return LineUnreaction, r, nil

		case "edit":
			e := EditLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
				Text:     UnescapeText(content),
			}
			return LineEdit, e, nil

		case "delete":
			d := DeleteLine{
				Ts:       ts,
				MsgID:    t.val,
				Sender:   sender,
				SenderID: senderID,
				Via:      via,
			}
			return LineDelete, d, nil
		}
	}

	return 0, nil, fmt.Errorf("parse line: no distinguishing tag (id/react/unreact/edit/delete): %q", truncate(line, 80))
}

// parseAttachValue parses "ATTACH_ID type=MIME_TYPE" into an Attachment.
func parseAttachValue(val string) Attachment {
	// val is e.g. "F07T3 type=image/jpeg"
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

// truncate shortens a string to at most n bytes for error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
