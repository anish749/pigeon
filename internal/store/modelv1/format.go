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

// FormatMsgLine renders a message line (without reactions) as display lines.
// loc controls the timezone for display (pass time.Local for user's timezone).
func FormatMsgLine(m MsgLine, loc *time.Location) []string {
	prefix := ""
	if m.Reply {
		prefix = "  "
	}

	tsStr := m.Ts.In(loc).Format(tsLayout)
	sender := displaySender(m.Sender, m.Via)
	var lines []string
	lines = append(lines, fmt.Sprintf("%s[%s] [%s] %s (%s): %s", prefix, tsStr, m.ID, sender, m.SenderID, m.Text))

	lines = append(lines, formatRaw(m.Raw, prefix+"    ")...)

	return lines
}

// FormatMsg renders a resolved message with its reactions as display lines.
// loc controls the timezone for display (pass time.Local for user's timezone).
func FormatMsg(m ResolvedMsg, loc *time.Location) []string {
	lines := FormatMsgLine(m.MsgLine, loc)

	if len(m.Reactions) > 0 {
		prefix := ""
		if m.Reply {
			prefix = "  "
		}
		lines = append(lines, prefix+"    "+formatReactions(m.Reactions))
	}

	return lines
}

// FormatMsgNotification renders a single message as channel-notification
// lines: sender and text on the first line (visible when truncated),
// bracketed metadata on the second.
//
// Operates on MsgLine only — reactions are rendered separately by
// FormatReactionNotification.
func FormatMsgNotification(m MsgLine, loc *time.Location, convMeta *ConvMeta) []string {
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
	meta += formatThreadTSMeta(m.ThreadTS)
	meta += formatThreadIDMeta(m.ThreadID)
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	lines = append(lines, meta)

	return lines
}

// formatThreadTSMeta returns the rendered " [thread_ts:<ts>]" segment, or
// an empty string when ts is empty. Slack's parent identifier is a TS, so
// notifications carrying Slack thread context emit this tag.
//
// Centralized so future per-line-type notification formatters (edit,
// delete, reaction) emit the same shape.
func formatThreadTSMeta(ts string) string {
	if ts == "" {
		return ""
	}
	return fmt.Sprintf(" [thread_ts:%s]", ts)
}

// formatThreadIDMeta returns the rendered " [thread_id:<id>]" segment, or
// an empty string when id is empty. Platforms whose parent identifier is
// not a timestamp (WhatsApp comments) emit this tag instead.
func formatThreadIDMeta(id string) string {
	if id == "" {
		return ""
	}
	return fmt.Sprintf(" [thread_id:%s]", id)
}

// FormatReactionNotification formats a single reaction event with parent
// context for Claude Code channel notifications. This does not include all
// reactions associated with the message — only the specific event being
// delivered. convMeta may be nil; when present, its tags are appended to
// the meta line so the agent can identify the conversation (type,
// channel ID, etc.) the same way it can on a message notification.
func FormatReactionNotification(m MsgLine, r ReactLine, loc *time.Location, convMeta *ConvMeta) []string {
	verb := "reacted with"
	if r.Remove {
		verb = "removed reaction"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%s %s :%s: to %s's %s",
		displaySender(r.Sender, r.Via), verb, r.Emoji, displaySender(m.Sender, m.Via), m.Text))
	lines = append(lines, formatRaw(m.Raw, "  ")...)

	meta := fmt.Sprintf("  [reaction] [%s] [message_id:%s] [sender_id:%s] [emoji:%s]",
		m.Ts.In(loc).Format("15:04:05"), m.ID, r.SenderID, r.Emoji)
	if r.Via != "" {
		meta += fmt.Sprintf(" [via:%s]", r.Via)
	}
	meta += formatThreadTSMeta(r.ThreadTS)
	meta += formatThreadIDMeta(r.ThreadID)
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	lines = append(lines, meta)

	return lines
}

// FormatReactionFallbackNotification formats a reaction notification for
// Claude Code when the original message could not be found. This happens
// when the reacted-to message is not on disk (e.g. older than synced
// history). convMeta is appended on the meta line when non-nil — same
// shape as FormatReactionNotification.
func FormatReactionFallbackNotification(r ReactLine, loc *time.Location, convMeta *ConvMeta) []string {
	verb := "reacted with"
	if r.Remove {
		verb = "removed reaction"
	}
	meta := fmt.Sprintf("  [reaction] [%s] [message_id:%s] [sender_id:%s] [emoji:%s]",
		r.Ts.In(loc).Format("15:04:05"), r.MsgID, r.SenderID, r.Emoji)
	meta += formatThreadTSMeta(r.ThreadTS)
	meta += formatThreadIDMeta(r.ThreadID)
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	return []string{
		fmt.Sprintf("%s %s :%s:", displaySender(r.Sender, r.Via), verb, r.Emoji),
		meta,
	}
}

// FormatEditNotification formats a single edit event for Claude Code
// channel notifications. The header reports the new text; the meta line
// carries [edit] plus the same identifying tags every event type emits
// (message_id, sender_id, optional via/thread_ts/thread_id, conv meta).
//
// The edit's own timestamp is the rendered HH:MM:SS — it identifies when
// the edit happened, not the original message's send time. The edited
// message's ID is in [message_id:...] so the agent can correlate.
func FormatEditNotification(e EditLine, loc *time.Location, convMeta *ConvMeta) []string {
	var lines []string
	lines = append(lines, fmt.Sprintf("%s edited message: %s",
		displaySender(e.Sender, e.Via), e.Text))
	lines = append(lines, formatRaw(e.Raw, "  ")...)

	meta := fmt.Sprintf("  [edit] [%s] [message_id:%s] [sender_id:%s]",
		e.Ts.In(loc).Format("15:04:05"), e.MsgID, e.SenderID)
	if e.Via != "" {
		meta += fmt.Sprintf(" [via:%s]", e.Via)
	}
	meta += formatThreadTSMeta(e.ThreadTS)
	meta += formatThreadIDMeta(e.ThreadID)
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	lines = append(lines, meta)
	return lines
}

// FormatDeleteNotification formats a single delete event for Claude Code
// channel notifications. The header reports who deleted which message;
// the meta line carries [delete] plus the same identifying tags as other
// event notifications (message_id, sender_id, optional via/thread).
func FormatDeleteNotification(d DeleteLine, loc *time.Location, convMeta *ConvMeta) []string {
	header := fmt.Sprintf("%s deleted message %s",
		displaySender(d.Sender, d.Via), d.MsgID)

	meta := fmt.Sprintf("  [delete] [%s] [message_id:%s] [sender_id:%s]",
		d.Ts.In(loc).Format("15:04:05"), d.MsgID, d.SenderID)
	if d.Via != "" {
		meta += fmt.Sprintf(" [via:%s]", d.Via)
	}
	meta += formatThreadTSMeta(d.ThreadTS)
	meta += formatThreadIDMeta(d.ThreadID)
	if convMeta != nil {
		if cm := FormatConvMeta(convMeta); cm != "" {
			meta += " " + cm
		}
	}
	return []string{header, meta}
}

// FormatDateFileNotification renders a resolved conversation day for Claude Code
// channel notifications. See FormatMsgNotification for per-message format.
// If any non-nil errors are passed, a warning line is appended at the end.
func FormatDateFileNotification(f *ResolvedDateFile, loc *time.Location, convMeta *ConvMeta, errs ...error) []string {
	if f == nil {
		return nil
	}
	var lines []string
	for _, m := range f.Messages {
		lines = append(lines, FormatMsgNotification(m.MsgLine, loc, convMeta)...)
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
