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
func prioritizeChannels(ctx context.Context, searcher messageSearcher, gate *rateLimitGate, cursors syncCursors, conversations []goslack.Channel) []goslack.Channel {
	// Sort all conversations: DMs first, then group IMs, private, public.
	sort.SliceStable(conversations, func(i, j int) bool {
		return channelPriority(conversations[i]) < channelPriority(conversations[j])
	})

	activeSet := discoverActiveChannels(ctx, searcher, gate, cursors, conversations)
	if activeSet == nil {
		// nil means "sync all" — no filtering possible or needed.
		return conversations
	}

	var toSync []goslack.Channel
	var skipped int
	for _, ch := range conversations {
		if activeSet.has(ch.ID) {
			toSync = append(toSync, ch)
		} else {
			skipped++
		}
	}

	slog.InfoContext(ctx, "sync priority: result",
		"to_sync", len(toSync), "skipped", skipped,
		"total", len(conversations), "reason", activeSet.reason())

	return toSync
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
	// Search is an optimization, not a requirement. If it fails (missing scope,
	// network error, rate limit), we fall back to syncing all channels — the same
	// behavior as before this optimization existed. No error is returned because
	// the caller's sync will still succeed, just without the filtering benefit.
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
			return syncAll(fmt.Sprintf("rate limit wait cancelled at page %d", page))
		}
		params.Page = page
		result, err = searcher.SearchMessagesContext(ctx, query, params)
		if err != nil {
			// Mid-pagination failure: we have a partial channel set that may be
			// missing active channels from unseen pages. Using it would silently
			// skip channels that had activity. Fall back to syncing all.
			slog.WarnContext(ctx, "sync priority: search page failed, syncing all",
				"page", page, "error", err)
			return syncAll(fmt.Sprintf("search page %d failed: %v", page, err))
		}

		for id := range collectChannelIDs(result.Matches) {
			active[id] = true
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

// channelPriority returns a sort key: DMs(0) < group IMs(1) < private channels(2) < public(3).
func channelPriority(ch goslack.Channel) int {
	if ch.IsIM {
		return 0
	}
	if ch.IsMpIM {
		return 1
	}
	if ch.IsPrivate {
		return 2
	}
	return 3
}
