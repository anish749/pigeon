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
				if !m.Msg.Ts.Before(cutoff) {
					count++
					if m.Msg.Sender != "" {
						senders[m.Msg.Sender] = struct{}{}
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
		for _, m := range g.msgs {
			resolved := modelv1.ResolvedMsg{MsgLine: m.Msg}
			for _, s := range modelv1.FormatMsg(resolved, time.Local) {
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
