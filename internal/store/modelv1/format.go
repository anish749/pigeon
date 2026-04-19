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

// FormatMsgNotification renders a message for Claude Code channel notifications.
// Sender and text lead (visible in truncated UI); metadata follows on an indented line.
//
// This function does not format reactions — it operates on MsgLine only.
func formatMsgNotification(m MsgLine, loc *time.Location, convMeta *ConvMeta) []string {
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

// FormatReactionNotification formats a message with a single reaction for
// Claude Code channel notifications. This does not include all reactions
// associated with the message — only the specific reaction event being delivered.
func FormatReactionNotification(m MsgLine, r ReactLine, loc *time.Location) []string {
	verb := "reacted with"
	if r.Remove {
		verb = "removed reaction"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%s %s :%s:", displaySender(r.Sender, r.Via), verb, r.Emoji))
	lines = append(lines, fmt.Sprintf("%s: %s", displaySender(m.Sender, m.Via), m.Text))
	lines = append(lines, formatRaw(m.Raw, "  ")...)

	meta := fmt.Sprintf("  [reaction] [%s] [message_id:%s] [sender_id:%s] [emoji:%s]",
		m.Ts.In(loc).Format("15:04:05"), m.ID, r.SenderID, r.Emoji)
	if r.Via != "" {
		meta += fmt.Sprintf(" [via:%s]", r.Via)
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
		lines = append(lines, formatMsgNotification(m.MsgLine, loc, convMeta)...)
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
