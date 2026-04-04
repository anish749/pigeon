package commands

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/paths"
	"github.com/anish/claude-msg-utils/internal/store"
)

type SearchParams struct {
	Query    string
	Platform string
	Account  string
	Since    string
}

func RunSearch(p SearchParams) error {
	var sinceDur time.Duration
	if p.Since != "" {
		d, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		sinceDur = d
	}

	results, err := store.SearchMessages(p.Query, p.Platform, p.Account, sinceDur)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	// Enrich results: replace phone senders with contact names,
	// and resolve conversation directory names to display names.
	enrichSearchResults(results, p.Platform, p.Account)

	var totalMatches int
	for _, r := range results {
		totalMatches += r.MatchCount
	}
	fmt.Printf("%d match(es) found:\n\n", totalMatches)
	printSearchSummary(results, sinceDur)
	printGroupedResults(results)
	return nil
}

func printGroupedResults(results []store.SearchResult) {
	type groupKey struct {
		platform, account, conversation string
	}
	type group struct {
		key      groupKey
		dates    []string
		sections [][]string // each section is a slice of lines
		matches  int
	}

	var order []groupKey
	groups := make(map[groupKey]*group)
	for _, r := range results {
		k := groupKey{r.Platform, r.Account, r.Conversation}
		g, ok := groups[k]
		if !ok {
			g = &group{key: k}
			groups[k] = g
			order = append(order, k)
		}
		g.sections = append(g.sections, r.Lines)
		g.matches += r.MatchCount
		g.dates = append(g.dates, r.Date)
	}

	for _, k := range order {
		g := groups[k]
		// Determine date range
		minDate, maxDate := g.dates[0], g.dates[0]
		for _, d := range g.dates[1:] {
			if d < minDate {
				minDate = d
			}
			if d > maxDate {
				maxDate = d
			}
		}
		dateStr := minDate
		if minDate != maxDate {
			dateStr = minDate + " to " + maxDate
		}

		dir := filepath.Join(paths.DataDir(), k.platform, k.account, k.conversation)
		fmt.Printf("%s/%s/%s (%s, %d matches)\n", k.platform, k.account, k.conversation, dateStr, g.matches)
		fmt.Printf("    %s\n", dir)
		for i, section := range g.sections {
			if i > 0 {
				fmt.Println("  ...")
			}
			for _, line := range section {
				fmt.Printf("  %s\n", line)
			}
		}
		fmt.Println()
	}
}

type timeBucket struct {
	dur   time.Duration
	label string
}

func printSearchSummary(results []store.SearchResult, sinceDur time.Duration) {
	if sinceDur == 0 || len(results) == 0 {
		return
	}

	now := time.Now()
	type msgInfo struct {
		ts     time.Time
		sender string
	}
	type groupKey struct {
		platform, account string
	}

	// Group messages by platform/account, preserving insertion order.
	groupMsgs := make(map[groupKey][]msgInfo)
	var groupOrder []groupKey
	for _, r := range results {
		k := groupKey{r.Platform, r.Account}
		if _, ok := groupMsgs[k]; !ok {
			groupOrder = append(groupOrder, k)
		}
		for _, line := range r.Lines {
			ts, sender := parseResultLine(line)
			if !ts.IsZero() {
				groupMsgs[k] = append(groupMsgs[k], msgInfo{ts: ts, sender: sender})
			}
		}
	}

	buckets := chooseBuckets(sinceDur)

	for _, k := range groupOrder {
		msgs := groupMsgs[k]
		if len(msgs) == 0 {
			continue
		}
		var lines []string
		for _, b := range buckets {
			if b.dur > sinceDur {
				continue
			}
			cutoff := now.Add(-b.dur)
			var count int
			senders := make(map[string]struct{})
			for _, m := range msgs {
				if !m.ts.Before(cutoff) {
					count++
					if m.sender != "" {
						senders[m.sender] = struct{}{}
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
		fmt.Printf("  %s/%s:\n", k.platform, k.account)
		for _, line := range lines {
			fmt.Println(line)
		}
		fmt.Println()
	}
}

func parseResultLine(line string) (time.Time, string) {
	if len(line) < 29 || line[0] != '[' {
		return time.Time{}, ""
	}
	ts, err := time.Parse("2006-01-02 15:04:05 -07:00", line[1:27])
	if err != nil {
		return time.Time{}, ""
	}
	rest := line[29:]
	idx := strings.Index(rest, ": ")
	if idx < 0 {
		return ts, ""
	}
	return ts, rest[:idx]
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
