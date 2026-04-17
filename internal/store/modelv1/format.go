package modelv1

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const tsLayout = "2006-01-02 15:04:05"

// displaySender decorates a sender name based on the via field.
func displaySender(sender string, via Via) string {
	switch via {
	case ViaPigeonAsBot:
		return "sent by pigeon"
	case ViaToPigeon:
		return "sent to pigeon by " + sender
	case ViaPigeonAsUser:
		return sender + " (via pigeon)"
	default:
		return sender
	}
}

// FormatMsg renders a resolved message with its reactions as display lines.
// loc controls the timezone for display (pass time.Local for user's timezone).
func FormatMsg(m ResolvedMsg, loc *time.Location) []string {
	prefix := ""
	if m.Reply {
		prefix = "  "
	}

	tsStr := m.Ts.In(loc).Format(tsLayout)
	sender := displaySender(m.Sender, m.Via)
	var lines []string
	lines = append(lines, fmt.Sprintf("%s[%s] [%s] %s (%s): %s", prefix, tsStr, m.ID, sender, m.SenderID, m.Text))

	lines = append(lines, formatRaw(m.Raw, prefix+"    ")...)

	if len(m.Reactions) > 0 {
		lines = append(lines, prefix+"    "+formatReactions(m.Reactions))
	}

	return lines
}

// formatMsgNotification renders a message for Claude Code channel notifications.
// Sender and text lead (visible in truncated UI); metadata follows on an indented line.
//
// TODO: include reactions
func formatMsgNotification(m ResolvedMsg, loc *time.Location, convMeta *ConvMeta) []string {
	tsStr := m.Ts.In(loc).Format("15:04:05")

	var lines []string
	lines = append(lines, fmt.Sprintf("%s: %s", displaySender(m.Sender, m.Via), m.Text))
	lines = append(lines, formatRaw(m.Raw, "  ")...)

	meta := fmt.Sprintf("  [%s] [message_id:%s] [sender_id:%s]", tsStr, m.ID, m.SenderID)
	if m.Via != "" {
		meta += fmt.Sprintf(" [via:%s]", m.Via)
	}
	if m.ReplyTo != "" {
		meta += fmt.Sprintf(" [reply_to:%s]", m.ReplyTo)
	}
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	lines = append(lines, meta)

	return lines
}

// FormatDateFileNotification renders a resolved conversation day for Claude Code
// channel notifications. See formatMsgNotification for per-message format.
// If any non-nil errors are passed, a warning line is appended at the end.
func FormatDateFileNotification(f *ResolvedDateFile, loc *time.Location, convMeta *ConvMeta, errs ...error) []string {
	if f == nil {
		return nil
	}
	var lines []string
	for _, m := range f.Messages {
		lines = append(lines, formatMsgNotification(m, loc, convMeta)...)
	}
	if w := formatWarning(errs...); w != "" {
		lines = append(lines, w)
	}
	return lines
}

// FormatDateFile renders a resolved conversation day as display lines.
// If any non-nil errors are passed, a warning line is appended at the end.
func FormatDateFile(f *ResolvedDateFile, loc *time.Location, errs ...error) []string {
	if f == nil {
		return nil
	}
	var lines []string
	for _, m := range f.Messages {
		lines = append(lines, FormatMsg(m, loc)...)
	}
	if w := formatWarning(errs...); w != "" {
		lines = append(lines, w)
	}
	return lines
}

// formatWarning joins non-nil errors into a single warning line.
// Returns empty string if all errors are nil.
func formatWarning(errs ...error) string {
	joined := errors.Join(errs...)
	if joined == nil {
		return ""
	}
	return "\u26a0 " + joined.Error()
}

// FormatConvMeta renders conversation metadata as a bracketed tag line.
// Returns empty string if there are no IDs to show.
func FormatConvMeta(meta *ConvMeta) string {
	var parts []string
	if meta.Type != "" {
		parts = append(parts, fmt.Sprintf("[type:%s]", meta.Type))
	}
	if meta.ChannelID != "" {
		parts = append(parts, fmt.Sprintf("[channel_id:%s]", meta.ChannelID))
	}
	if meta.UserID != "" {
		parts = append(parts, fmt.Sprintf("[user_id:%s]", meta.UserID))
	}
	if meta.JID != "" {
		parts = append(parts, fmt.Sprintf("[jid:%s]", meta.JID))
	}
	if meta.LID != "" {
		parts = append(parts, fmt.Sprintf("[lid:%s]", meta.LID))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// formatRaw renders Slack-specific raw content (attachments, files) as
// indented display lines below the message. Returns nil if the message has
// no raw content worth rendering.
func formatRaw(raw map[string]any, indent string) []string {
	if len(raw) == 0 {
		return nil
	}
	var lines []string
	lines = append(lines, formatRawAttachments(raw, indent)...)
	lines = append(lines, formatRawFiles(raw, indent)...)
	return lines
}

// formatRawAttachments renders Slack attachments (Jira unfurls, Jenkins
// notifications, etc.) as indented lines showing fallback text and fields.
func formatRawAttachments(raw map[string]any, indent string) []string {
	attsRaw, ok := raw["attachments"]
	if !ok {
		return nil
	}
	atts, ok := attsRaw.([]any)
	if !ok {
		return nil
	}
	var lines []string
	for _, a := range atts {
		att, ok := a.(map[string]any)
		if !ok {
			continue
		}
		fallback, _ := att["fallback"].(string)
		if fallback == "" {
			// Try pretext as fallback.
			fallback, _ = att["pretext"].(string)
		}
		if fallback == "" || fallback == "[no preview available]" {
			continue
		}
		// Render each line of multi-line fallback text indented.
		for i, fline := range strings.Split(fallback, "\n") {
			if fline == "" {
				continue
			}
			if i == 0 {
				lines = append(lines, indent+"📎 "+fline)
			} else {
				lines = append(lines, indent+"   "+fline)
			}
		}

		// Render fields (e.g. Assignee, Priority) on a second line.
		if fields := formatAttachmentFields(att); fields != "" {
			lines = append(lines, indent+"   "+fields)
		}
	}
	return lines
}

// formatAttachmentFields renders attachment fields as "Key: Value · Key: Value".
// Fields whose value duplicates the fallback text are skipped (some bots like
// Jenkins put the same content in both).
func formatAttachmentFields(att map[string]any) string {
	fieldsRaw, ok := att["fields"]
	if !ok {
		return ""
	}
	fields, ok := fieldsRaw.([]any)
	if !ok {
		return ""
	}
	fallback, _ := att["fallback"].(string)
	var parts []string
	for _, f := range fields {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		title, _ := field["title"].(string)
		value, _ := field["value"].(string)
		if title == "" && value == "" {
			continue
		}
		if value == fallback {
			continue
		}
		if title != "" {
			parts = append(parts, title+": "+value)
		} else {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " · ")
}

// formatRawFiles renders Slack file attachments as indented lines showing
// file name, MIME type, human-readable size, and a permalink URL.
func formatRawFiles(raw map[string]any, indent string) []string {
	filesRaw, ok := raw["files"]
	if !ok {
		return nil
	}
	files, ok := filesRaw.([]any)
	if !ok {
		return nil
	}
	var lines []string
	for _, f := range files {
		file, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := file["name"].(string)
		if name == "" {
			name, _ = file["title"].(string)
		}
		if name == "" {
			name = "unnamed file"
		}
		mime, _ := file["mimetype"].(string)
		var sizePart string
		if size, ok := file["size"].(float64); ok && size > 0 {
			sizePart = humanSize(int64(size))
		}

		info := name
		if mime != "" || sizePart != "" {
			var meta []string
			if mime != "" {
				meta = append(meta, mime)
			}
			if sizePart != "" {
				meta = append(meta, sizePart)
			}
			info += " (" + strings.Join(meta, ", ") + ")"
		}

		line := indent + "📄 " + info
		if permalink, _ := file["permalink"].(string); permalink != "" {
			line += "\n" + indent + "   " + permalink
		}
		lines = append(lines, line)
	}
	return lines
}

// humanSize formats a byte count as a human-readable string.
func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}


// formatReactions renders a list of reactions as a single display line.
// e.g. "👍 Bob, Charlie · 🎉 Dave"
func formatReactions(reactions []ReactLine) string {
	type emojiGroup struct {
		emoji string
		users []string
	}
	var order []string
	groups := make(map[string]*emojiGroup)
	for _, r := range reactions {
		g, ok := groups[r.Emoji]
		if !ok {
			g = &emojiGroup{emoji: r.Emoji}
			groups[r.Emoji] = g
			order = append(order, r.Emoji)
		}
		g.users = append(g.users, r.Sender)
	}

	var parts []string
	for _, emoji := range order {
		g := groups[emoji]
		sort.Strings(g.users)
		parts = append(parts, g.emoji+" "+strings.Join(g.users, ", "))
	}
	return strings.Join(parts, " · ")
}
