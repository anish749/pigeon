package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/storev1"
)

type SearchParams struct {
	Query    string
	Platform string
	Account  string
	Since    string
}

func RunSearch(p SearchParams) error {
	s := storev1.NewFSStore(paths.DataDir())

	var sinceDur time.Duration
	if p.Since != "" {
		d, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		sinceDur = d
	}

	opts := storev1.SearchOpts{
		Platform: p.Platform,
		Account:  p.Account,
		Since:    sinceDur,
	}
	if p.Platform != "" && p.Account != "" {
		a := account.New(p.Platform, p.Account)
		opts.Account = a.NameSlug()
	}

	results, err := s.Search(p.Query, opts)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	var totalMatches int
	for _, r := range results {
		totalMatches += r.MatchCount
	}
	fmt.Printf("%d match(es) found:\n\n", totalMatches)
	printSearchSummaryV1(results, sinceDur)
	printGroupedResultsV1(results)
	return nil
}

func printGroupedResultsV1(results []storev1.SearchResult) {
	type groupKey struct {
		platform, account, conversation string
	}
	type group struct {
		key      groupKey
		dates    []string
		sections [][]modelv1.ResolvedMsg
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
		g.sections = append(g.sections, r.Messages)
		g.matches += r.MatchCount
		g.dates = append(g.dates, r.Date)
	}

	for _, k := range order {
		g := groups[k]
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

		a := account.New(k.platform, k.account)
		dir := a.ConversationDir(k.conversation)
		fmt.Printf("%s/%s (%s, %d matches)\n", a.Display(), k.conversation, dateStr, g.matches)
		fmt.Printf("    %s\n", dir)
		for i, section := range g.sections {
			if i > 0 {
				fmt.Println("  ...")
			}
			for _, m := range section {
				for _, s := range modelv1.FormatMsg(m, time.Local, true) {
					fmt.Printf("  %s\n", s)
				}
			}
		}
		fmt.Println()
	}
}

func printSearchSummaryV1(results []storev1.SearchResult, sinceDur time.Duration) {
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

	groupMsgs := make(map[groupKey][]msgInfo)
	var groupOrder []groupKey
	for _, r := range results {
		k := groupKey{r.Platform, r.Account}
		if _, ok := groupMsgs[k]; !ok {
			groupOrder = append(groupOrder, k)
		}
		for _, m := range r.Messages {
			groupMsgs[k] = append(groupMsgs[k], msgInfo{ts: m.Ts, sender: m.Sender})
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
