package slack

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	goslack "github.com/slack-go/slack"
)

const (
	// minChannelsForSearch is the minimum number of channels before we bother
	// querying search. Below this, syncing all channels is fast enough.
	minChannelsForSearch = 50

	// searchPageSize is the number of results per search page (max 100).
	searchPageSize = 100

	// activeRatioThreshold is the fraction of total channels that can be active
	// before we give up filtering and sync all. If >65% of channels are active,
	// the search overhead isn't saving much.
	activeRatioThreshold = 0.65
)

// messageSearcher abstracts the Slack search API for testability.
type messageSearcher interface {
	SearchMessagesContext(ctx context.Context, query string, params goslack.SearchParameters) (*goslack.SearchMessages, error)
}

// prioritizeChannels takes the full set of member conversations and returns
// the subset that needs syncing, sorted by type (DMs → mpims → private → public).
//
// On first sync (no cursors) or for small workspaces, all channels are returned.
// On subsequent syncs, search.messages discovers which channels had activity
// since the last sync, and only those are returned.
//
// All conversations are registered in the resolver regardless of whether they
// are selected for sync — the real-time listener needs the full membership set.
func prioritizeChannels(ctx context.Context, searcher messageSearcher, gate *rateLimitGate, cursors syncCursors, conversations []goslack.Channel) (toSync []goslack.Channel, skipped int, reason string) {
	// Sort all conversations: DMs first, then group IMs, private, public.
	sort.SliceStable(conversations, func(i, j int) bool {
		return channelPriority(conversations[i]) < channelPriority(conversations[j])
	})

	activeSet := discoverActiveChannels(ctx, searcher, gate, cursors, conversations)
	if activeSet == nil {
		// nil means "sync all" — no filtering possible or needed.
		return conversations, 0, activeSet.reason()
	}

	for _, ch := range conversations {
		if activeSet.has(ch.ID) {
			toSync = append(toSync, ch)
		} else {
			skipped++
		}
	}
	return toSync, skipped, activeSet.reason()
}

// activeChannelSet holds the result of the search-based discovery.
// nil means "sync all channels" (first sync, small workspace, search failed, etc).
type activeChannelSet struct {
	ids          map[string]bool
	searchTotal  int
	reasonString string
}

func (a *activeChannelSet) has(id string) bool {
	return a.ids[id]
}

func (a *activeChannelSet) reason() string {
	if a == nil {
		return ""
	}
	return a.reasonString
}

// syncAll returns nil to signal that all channels should be synced.
func syncAll(reason string) *activeChannelSet {
	// We log the reason here so the caller just sees nil = sync all.
	slog.Info("sync priority: sync all", "reason", reason)
	return nil
}

// discoverActiveChannels queries search.messages to find which channels had
// activity since the most recent cursor. Returns nil if all channels should
// be synced (first sync, few channels, search error, too many active).
func discoverActiveChannels(ctx context.Context, searcher messageSearcher, gate *rateLimitGate, cursors syncCursors, conversations []goslack.Channel) *activeChannelSet {
	if len(cursors) == 0 {
		return syncAll("first sync, no cursors")
	}

	if len(conversations) < minChannelsForSearch {
		return syncAll(fmt.Sprintf("only %d channels, below threshold", len(conversations)))
	}

	maxCursor := maxCursorTime(cursors)
	if maxCursor.IsZero() {
		return syncAll("no valid cursor timestamps")
	}

	// Subtract one day because search uses day-level granularity
	// (after:YYYY-MM-DD means "after end of that day").
	afterDate := maxCursor.Add(-24 * time.Hour).Format("2006-01-02")
	query := "after:" + afterDate

	if err := gate.wait(ctx); err != nil {
		return syncAll("rate limit wait cancelled")
	}

	params := goslack.SearchParameters{
		Sort:          "timestamp",
		SortDirection: "desc",
		Count:         searchPageSize,
		Page:          1,
	}
	result, err := searcher.SearchMessagesContext(ctx, query, params)
	if err != nil {
		slog.WarnContext(ctx, "sync priority: search failed, syncing all",
			"error", err, "query", query)
		return syncAll(fmt.Sprintf("search failed: %v", err))
	}

	if result.Total == 0 {
		reason := fmt.Sprintf("search returned 0 results for %q", query)
		slog.InfoContext(ctx, "sync priority: no activity", "query", query)
		return &activeChannelSet{
			ids:          make(map[string]bool),
			reasonString: reason,
		}
	}

	active := collectChannelIDs(result.Matches)
	searchTotal := result.Total
	totalPages := (searchTotal + searchPageSize - 1) / searchPageSize
	totalChannels := len(conversations)

	slog.InfoContext(ctx, "sync priority: search page 1",
		"query", query, "total_messages", searchTotal,
		"total_pages", totalPages, "active_channels", len(active),
		"total_channels", totalChannels)

	if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
		return syncAll(fmt.Sprintf("active channels %d/%d exceeds %.0f%% threshold after page 1",
			len(active), totalChannels, activeRatioThreshold*100))
	}

	for page := 2; page <= totalPages; page++ {
		if err := gate.wait(ctx); err != nil {
			break
		}
		params.Page = page
		result, err = searcher.SearchMessagesContext(ctx, query, params)
		if err != nil {
			slog.WarnContext(ctx, "sync priority: search page failed, using results so far",
				"page", page, "error", err)
			break
		}

		prevCount := len(active)
		for id := range collectChannelIDs(result.Matches) {
			active[id] = true
		}
		newOnPage := len(active) - prevCount

		slog.InfoContext(ctx, "sync priority: search page",
			"page", page, "new_channels", newOnPage,
			"active_channels", len(active))

		if newOnPage == 0 {
			break
		}

		if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
			return syncAll(fmt.Sprintf("active channels %d/%d exceeds %.0f%% threshold at page %d",
				len(active), totalChannels, activeRatioThreshold*100, page))
		}
	}

	reason := fmt.Sprintf("search found %d active channels out of %d (%d messages, query=%q)",
		len(active), totalChannels, searchTotal, query)
	slog.InfoContext(ctx, "sync priority: filtered", "reason", reason)

	return &activeChannelSet{
		ids:          active,
		searchTotal:  searchTotal,
		reasonString: reason,
	}
}

// maxCursorTime finds the most recent cursor timestamp across all channels.
func maxCursorTime(cursors syncCursors) time.Time {
	var max time.Time
	for _, ts := range cursors {
		t := ParseTimestamp(ts)
		if t.After(max) {
			max = t
		}
	}
	return max
}

// collectChannelIDs extracts unique channel IDs from search matches.
func collectChannelIDs(matches []goslack.SearchMessage) map[string]bool {
	ids := make(map[string]bool, len(matches))
	for _, m := range matches {
		if m.Channel.ID != "" {
			ids[m.Channel.ID] = true
		}
	}
	return ids
}
