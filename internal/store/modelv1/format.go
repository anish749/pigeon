package modelv1

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const tsLayout = "2006-01-02 15:04:05"

// FormatMsg renders a resolved message with its reactions as display lines.
// loc controls the timezone for display (pass time.Local for user's timezone).
func FormatMsg(m ResolvedMsg, loc *time.Location) []string {
	prefix := ""
	if m.Reply {
		prefix = "  "
	}

	tsStr := m.Ts.In(loc).Format(tsLayout)
	var lines []string
	lines = append(lines, fmt.Sprintf("%s[%s] [%s] %s (%s): %s", prefix, tsStr, m.ID, m.Sender, m.SenderID, m.Text))

	if len(m.Reactions) > 0 {
		lines = append(lines, prefix+"    "+formatReactions(m.Reactions))
	}

	return lines
}

// FormatDateFile renders a resolved conversation day as display lines.
func FormatDateFile(f *ResolvedDateFile, loc *time.Location) []string {
	if f == nil {
		return nil
	}
	var lines []string
	for _, m := range f.Messages {
		lines = append(lines, FormatMsg(m, loc)...)
	}
	return lines
}

// FormatThreadFile renders a resolved thread as display lines.
func FormatThreadFile(f *ResolvedThreadFile, loc *time.Location) []string {
	if f == nil {
		return nil
	}
	var lines []string

	for _, c := range f.Before {
		lines = append(lines, FormatMsg(c, loc)...)
	}

	lines = append(lines, FormatMsg(f.Parent, loc)...)

	for _, r := range f.Replies {
		lines = append(lines, FormatMsg(r, loc)...)
	}

	for _, c := range f.After {
		lines = append(lines, FormatMsg(c, loc)...)
	}

	return lines
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
