package search

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// PrintSummary prints time-bucketed match counts grouped by platform/account.
// Only prints when sinceDur > 0 and there are matches.
func PrintSummary(matches []Match, sinceDur time.Duration) {
	if sinceDur == 0 || len(matches) == 0 {
		return
	}

	now := time.Now()
	type groupKey struct{ platform, account string }

	groupMsgs := make(map[groupKey][]Match)
	var groupOrder []groupKey
	for _, m := range matches {
		k := groupKey{m.Platform, m.Account}
		if _, ok := groupMsgs[k]; !ok {
			groupOrder = append(groupOrder, k)
		}
		groupMsgs[k] = append(groupMsgs[k], m)
	}

	buckets := chooseBuckets(sinceDur)

	for _, k := range groupOrder {
		msgs := groupMsgs[k]
		var lines []string
		for _, b := range buckets {
			if b.dur > sinceDur {
				continue
			}
			cutoff := now.Add(-b.dur)
			var count int
			senders := make(map[string]struct{})
			for _, m := range msgs {
				if !m.Line.Ts().Before(cutoff) {
					count++
					if s := lineSender(m.Line); s != "" {
						senders[s] = struct{}{}
					}
				}
			}
			if count > 0 {
				lines = append(lines, fmt.Sprintf("    Last %-4s %3d msgs — %s", b.label+":", count, formatSenders(senders, 50)))
			}
		}
		if len(lines) == 0 {
			continue
		}
		label := k.account
		if k.platform != "" {
			label = k.platform + "/" + k.account
		}
		fmt.Printf("  %s:\n", label)
		for _, line := range lines {
			fmt.Println(line)
		}
		fmt.Println()
	}
}

// PrintGroupedResults prints matches grouped by conversation with formatted messages.
func PrintGroupedResults(matches []Match) {
	type groupKey struct{ platform, account, conversation string }
	type group struct {
		key     groupKey
		dates   map[string]bool
		msgs    []Match
		matches int
	}

	var order []groupKey
	groups := make(map[groupKey]*group)
	for _, m := range matches {
		k := groupKey{m.Platform, m.Account, m.Conversation}
		g, ok := groups[k]
		if !ok {
			g = &group{key: k, dates: make(map[string]bool)}
			groups[k] = g
			order = append(order, k)
		}
		g.msgs = append(g.msgs, m)
		g.matches++
		g.dates[m.Date] = true
	}

	for _, k := range order {
		g := groups[k]
		dates := sortedKeys(g.dates)
		dateStr := dates[0]
		if len(dates) > 1 {
			dateStr = dates[0] + " to " + dates[len(dates)-1]
		}

		label := k.conversation
		if k.account != "" {
			label = k.account + "/" + k.conversation
		}
		if k.platform != "" {
			label = k.platform + "/" + label
		}
		fmt.Printf("%s (%s, %d matches)\n", label, dateStr, g.matches)

		// Show unique file paths for this conversation.
		filePaths := make(map[string]bool)
		for _, m := range g.msgs {
			if m.FilePath != "" {
				filePaths[m.FilePath] = true
			}
		}
		for _, fp := range sortedKeys(filePaths) {
			fmt.Printf("  %s\n", fp)
		}

		for _, m := range g.msgs {
			for _, s := range formatMatchLine(m.Line, time.Local) {
				fmt.Printf("  %s\n", s)
			}
		}
		fmt.Println()
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type timeBucket struct {
	dur   time.Duration
	label string
}

func chooseBuckets(since time.Duration) []timeBucket {
	h := since.Hours()
	if h <= 6 {
		return []timeBucket{
			{1 * time.Hour, "1h"},
			{2 * time.Hour, "2h"},
			{3 * time.Hour, "3h"},
			{6 * time.Hour, "6h"},
		}
	}
	if h <= 24 {
		return []timeBucket{
			{1 * time.Hour, "1h"},
			{3 * time.Hour, "3h"},
			{6 * time.Hour, "6h"},
			{12 * time.Hour, "12h"},
			{24 * time.Hour, "24h"},
		}
	}
	if h <= 72 {
		return []timeBucket{
			{3 * time.Hour, "3h"},
			{6 * time.Hour, "6h"},
			{12 * time.Hour, "12h"},
			{24 * time.Hour, "1d"},
			{48 * time.Hour, "2d"},
			{72 * time.Hour, "3d"},
		}
	}
	if h <= 7*24 {
		return []timeBucket{
			{12 * time.Hour, "12h"},
			{24 * time.Hour, "1d"},
			{3 * 24 * time.Hour, "3d"},
			{5 * 24 * time.Hour, "5d"},
			{7 * 24 * time.Hour, "7d"},
		}
	}
	return []timeBucket{
		{24 * time.Hour, "1d"},
		{3 * 24 * time.Hour, "3d"},
		{7 * 24 * time.Hour, "7d"},
		{14 * 24 * time.Hour, "14d"},
		{30 * 24 * time.Hour, "30d"},
	}
}

// lineSender returns the display name of the sender for any displayable line type.
func lineSender(l modelv1.Line) string {
	switch l.Type {
	case modelv1.LineMessage:
		return l.Msg.Sender
	case modelv1.LineEmail:
		if l.Email.FromName != "" {
			return l.Email.FromName
		}
		return l.Email.From
	case modelv1.LineComment:
		if l.Comment.Runtime.Author != nil {
			return l.Comment.Runtime.Author.DisplayName
		}
	case modelv1.LineEvent:
		if l.Event.Runtime.Organizer != nil {
			return l.Event.Runtime.Organizer.DisplayName
		}
	}
	return ""
}

// formatMatchLine renders any displayable line type for terminal output.
func formatMatchLine(l modelv1.Line, loc *time.Location) []string {
	switch l.Type {
	case modelv1.LineMessage:
		return modelv1.FormatMsgLine(*l.Msg, loc)
	case modelv1.LineEmail:
		tsStr := l.Email.Ts.In(loc).Format("2006-01-02 15:04:05")
		sender := l.Email.FromName
		if sender == "" {
			sender = l.Email.From
		}
		return []string{fmt.Sprintf("[%s] %s: %s", tsStr, sender, l.Email.Subject)}
	case modelv1.LineComment:
		author := ""
		if l.Comment.Runtime.Author != nil {
			author = l.Comment.Runtime.Author.DisplayName
		}
		return []string{fmt.Sprintf("[comment] %s: %s", author, l.Comment.Runtime.Content)}
	case modelv1.LineEvent:
		return []string{fmt.Sprintf("[event] %s (%s)", l.Event.Runtime.Summary, l.Event.Runtime.Status)}
	case modelv1.LineLinearIssue:
		return []string{fmt.Sprintf("[linear] %s", l.Issue.Runtime.Identifier)}
	case modelv1.LineLinearComment:
		return []string{fmt.Sprintf("[linear-comment] %s", l.LinearComment.Runtime.ID)}
	default:
		return nil
	}
}

func formatSenders(senders map[string]struct{}, max int) string {
	names := make([]string, 0, len(senders))
	for name := range senders {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + ", ..."
}
