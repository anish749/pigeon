package slack

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	goslack "github.com/slack-go/slack"
)

// syncPriority determines which channels need syncing by querying the Slack
// search API to discover channels with recent activity.
//
// Decision tree:
//
//  1. No cursors (first sync) → sync all channels.
//  2. Few channels (< minChannelsForSearch) → sync all, not worth optimizing.
//  3. Query search.messages with after:<date> derived from the most recent cursor.
//     a. Total == 0 → no activity, skip sync entirely.
//     b. Paginate, collecting unique channel IDs. Stop when a page adds no new IDs
//        or the active set exceeds activeRatioThreshold of total channels.
//     c. If active set is large (> activeRatioThreshold) → sync all.
//     d. Otherwise → sync only the active set.
type syncPriority struct {
	// activeChannels is the set of channel IDs with recent activity.
	// nil means "sync all" (no filtering).
	activeChannels map[string]bool

	// searchTotal is the total number of messages the search found.
	searchTotal int

	// reason describes why this priority was chosen (for logging).
	reason string
}

// shouldSync reports whether a channel should be synced.
func (p *syncPriority) shouldSync(channelID string) bool {
	if p.activeChannels == nil {
		return true // sync all
	}
	return p.activeChannels[channelID]
}

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

// computeSyncPriority determines which channels need syncing by using
// search.messages to discover recent activity.
//
// cursors is the current sync cursor state. conversations is the full list
// of channels to sync (already filtered to member channels).
func computeSyncPriority(ctx context.Context, searcher messageSearcher, gate *rateLimitGate, cursors syncCursors, conversations []goslack.Channel) *syncPriority {
	// Case 1: No cursors — first sync, must check everything.
	if len(cursors) == 0 {
		return &syncPriority{reason: "first sync, no cursors"}
	}

	// Case 2: Few channels — not worth the search overhead.
	if len(conversations) < minChannelsForSearch {
		return &syncPriority{reason: fmt.Sprintf("only %d channels, below threshold", len(conversations))}
	}

	// Derive the after: date from the most recent cursor. This represents the
	// last time any channel was synced. Subtract one day because search.messages
	// uses day-level granularity (after:YYYY-MM-DD means "after end of that day").
	maxCursor := maxCursorTime(cursors)
	if maxCursor.IsZero() {
		return &syncPriority{reason: "no valid cursor timestamps"}
	}
	afterDate := maxCursor.Add(-24 * time.Hour).Format("2006-01-02")
	query := "after:" + afterDate

	// First page: discover total and start collecting active channel IDs.
	if err := gate.wait(ctx); err != nil {
		return &syncPriority{reason: "rate limit wait cancelled"}
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
		return &syncPriority{reason: fmt.Sprintf("search failed: %v", err)}
	}

	// Case: no activity since the cursor date.
	if result.Total == 0 {
		return &syncPriority{
			activeChannels: make(map[string]bool),
			searchTotal:    0,
			reason:         fmt.Sprintf("search returned 0 results for %q", query),
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

	// If already over the active ratio threshold, sync all.
	if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
		return &syncPriority{
			searchTotal: searchTotal,
			reason: fmt.Sprintf("active channels %d/%d exceeds %.0f%% threshold after page 1",
				len(active), totalChannels, activeRatioThreshold*100),
		}
	}

	// Paginate remaining pages, collecting new channel IDs.
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

		// Stop if this page added no new channels — we've plateaued.
		if newOnPage == 0 {
			break
		}

		// Stop if active set crossed the threshold.
		if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
			return &syncPriority{
				searchTotal: searchTotal,
				reason: fmt.Sprintf("active channels %d/%d exceeds %.0f%% threshold at page %d",
					len(active), totalChannels, activeRatioThreshold*100, page),
			}
		}
	}

	return &syncPriority{
		activeChannels: active,
		searchTotal:    searchTotal,
		reason: fmt.Sprintf("search found %d active channels out of %d (%d messages, query=%q)",
			len(active), totalChannels, searchTotal, query),
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
