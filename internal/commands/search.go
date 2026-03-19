package commands

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("q", "", "search query [required]")
	platform := fs.String("platform", "", "filter by platform")
	account := fs.String("account", "", "filter by account")
	since := fs.String("since", "", "only search messages from last duration (e.g. 2h, 7d)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *query == "" {
		return fmt.Errorf("required flag: -q")
	}

	var sinceDur time.Duration
	if *since != "" {
		d, err := parseDuration(*since)
		if err != nil {
			return fmt.Errorf("invalid -since value %q: %w", *since, err)
		}
		sinceDur = d
	}

	results, err := store.SearchMessages(*query, *platform, *account, sinceDur)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No matches found.")
		return nil
	}

	fmt.Printf("%d match(es) found:\n\n", len(results))
	printSearchSummary(results, sinceDur)
	for _, r := range results {
		dir := filepath.Join(store.DataDir(), r.Platform, r.Account, r.Conversation)
		fmt.Printf("[%s/%s/%s %s]\n    %s\n  %s\n\n", r.Platform, r.Account, r.Conversation, r.Date, dir, r.Line)
	}
	return nil
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
	var msgs []msgInfo
	for _, r := range results {
		ts, sender := parseResultLine(r.Line)
		if !ts.IsZero() {
			msgs = append(msgs, msgInfo{ts: ts, sender: sender})
		}
	}
	if len(msgs) == 0 {
		return
	}

	buckets := chooseBuckets(sinceDur)

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
			fmt.Printf("  Last %-4s %3d msgs — %s\n", b.label+":", count, formatSenders(senders, 50))
		}
	}
	fmt.Println()
}

func parseResultLine(line string) (time.Time, string) {
	if len(line) < 22 || line[0] != '[' {
		return time.Time{}, ""
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", line[1:20], time.Local)
	if err != nil {
		return time.Time{}, ""
	}
	rest := line[22:]
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
