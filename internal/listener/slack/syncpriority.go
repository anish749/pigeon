package slack

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/store"
)

const (
	// minChannelsForSearch is the minimum number of channels before we bother
	// querying search. Below this, syncing all channels is fast enough.
	minChannelsForSearch = 75

	// searchPageSize is the number of results per search page (max 100).
	searchPageSize = 100

	// activeRatioThreshold is the fraction of total channels that can be active
	// before we give up filtering and sync all. If >65% of channels are active,
	// the search overhead isn't saving much.
	activeRatioThreshold = 0.65
)

// slackPrioritizer abstracts Slack APIs used for sync prioritization.
type slackPrioritizer interface {
	SearchMessagesContext(ctx context.Context, query string, params goslack.SearchParameters) (*goslack.SearchMessages, error)
	GetUserPrefsContext(ctx context.Context) (*goslack.UserPrefsCarrier, error)
}

// prioritizeChannels takes the full set of member conversations and returns
// the subset that needs syncing, sorted by type (DMs → mpims → private → public).
//
// On first sync (no cursors) or for small workspaces, all channels are returned.
// On subsequent syncs, search.messages discovers which channels had activity
// since the last sync, and only those are returned.
func prioritizeChannels(ctx context.Context, api slackPrioritizer, gate *rateLimitGate, cursors store.SlackCursors, conversations []goslack.Channel) []goslack.Channel {
	// Filter out muted channels before any other prioritization.
	conversations = filterMuted(ctx, api, conversations)

	// Sort all conversations: DMs first, then group IMs, private, public.
	sort.SliceStable(conversations, func(i, j int) bool {
		return channelPriority(conversations[i]) < channelPriority(conversations[j])
	})

	activeSet := discoverActiveChannels(ctx, api, gate, cursors, conversations)
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
	slog.Info("sync priority: sync all", "reason", reason)
	return nil
}

// discoverActiveChannels queries search.messages to find which channels had
// activity since the most recent cursor. Returns nil if all channels should
// be synced (first sync, few channels, search error, too many active).
func discoverActiveChannels(ctx context.Context, searcher slackPrioritizer, gate *rateLimitGate, cursors store.SlackCursors, conversations []goslack.Channel) *activeChannelSet {
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

	active, total, err := searchActiveChannelIDs(ctx, searcher, gate, query, len(conversations))
	if err != nil {
		return syncAll(fmt.Sprintf("search failed: %v", err))
	}

	// nil means searchActiveChannelIDs decided to sync all (threshold exceeded
	// or mid-pagination error). Distinct from empty map which means no activity.
	if active == nil {
		return syncAll(fmt.Sprintf("search found too many active channels (%d messages, query=%q)", total, query))
	}

	if len(active) == 0 {
		reason := fmt.Sprintf("search returned 0 results for %q", query)
		slog.InfoContext(ctx, "sync priority: no activity", "query", query)
		return &activeChannelSet{ids: active, reasonString: reason}
	}

	reason := fmt.Sprintf("search found %d active channels out of %d (%d messages, query=%q)",
		len(active), len(conversations), total, query)
	slog.InfoContext(ctx, "sync priority: filtered", "reason", reason)

	return &activeChannelSet{
		ids:          active,
		searchTotal:  total,
		reasonString: reason,
	}
}

// searchActiveChannelIDs paginates through search.messages results, collecting
// unique channel IDs that had activity. Returns nil if all channels should be
// synced (too many active, or a mid-pagination error).
func searchActiveChannelIDs(ctx context.Context, searcher slackPrioritizer, gate *rateLimitGate, query string, totalChannels int) (map[string]bool, int, error) {
	params := goslack.SearchParameters{
		Sort:          "timestamp",
		SortDirection: "desc",
		Count:         searchPageSize,
		Page:          1,
	}

	// Search is an optimization, not a requirement. If it fails (missing scope,
	// network error), we fall back to syncing all channels — the same behavior
	// as before this optimization existed.
	result, err := searchWithRetry(ctx, searcher, gate, query, params)
	if err != nil {
		return nil, 0, err
	}

	if result.Total == 0 {
		return make(map[string]bool), 0, nil
	}

	active := collectChannelIDs(result.Matches)
	totalPages := (result.Total + searchPageSize - 1) / searchPageSize

	slog.InfoContext(ctx, "sync priority: search page 1",
		"query", query, "total_messages", result.Total,
		"total_pages", totalPages, "active_channels", len(active),
		"total_channels", totalChannels)

	if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
		return nil, result.Total, nil
	}

	for page := 2; page <= totalPages; page++ {
		params.Page = page
		pageResult, err := searchWithRetry(ctx, searcher, gate, query, params)
		if err != nil {
			// Mid-pagination failure: partial channel set may be missing active
			// channels from unseen pages. Fall back to syncing all.
			return nil, result.Total, nil
		}

		for id := range collectChannelIDs(pageResult.Matches) {
			active[id] = true
		}

		if float64(len(active)) > activeRatioThreshold*float64(totalChannels) {
			return nil, result.Total, nil
		}
	}

	return active, result.Total, nil
}

// searchWithRetry calls SearchMessagesContext, retrying on rate limit errors.
func searchWithRetry(ctx context.Context, searcher slackPrioritizer, gate *rateLimitGate, query string, params goslack.SearchParameters) (*goslack.SearchMessages, error) {
	for {
		if err := gate.wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}
		result, err := searcher.SearchMessagesContext(ctx, query, params)
		if gate.update(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

// maxCursorTime finds the most recent cursor timestamp across all channels.
func maxCursorTime(cursors store.SlackCursors) time.Time {
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

// filterMuted removes muted channels from the conversation list.
// If prefs is nil or the fetch fails, returns conversations unchanged.
func filterMuted(ctx context.Context, api slackPrioritizer, conversations []goslack.Channel) []goslack.Channel {
	muted, err := fetchMutedChannels(ctx, api)
	if err != nil {
		slog.WarnContext(ctx, "sync priority: failed to fetch muted channels, skipping mute filter", "error", err)
		return conversations
	}
	if len(muted) == 0 {
		return conversations
	}
	var filtered []goslack.Channel
	var skipped int
	for _, ch := range conversations {
		if muted[ch.ID] {
			skipped++
			continue
		}
		filtered = append(filtered, ch)
	}
	if skipped > 0 {
		slog.InfoContext(ctx, "sync priority: skipped muted channels", "skipped", skipped)
	}
	return filtered
}

// fetchMutedChannels returns a set of channel IDs that the user has muted.
func fetchMutedChannels(ctx context.Context, api slackPrioritizer) (map[string]bool, error) {
	carrier, err := api.GetUserPrefsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get user prefs: %w", err)
	}
	if carrier.UserPrefs == nil {
		return nil, nil
	}
	raw := carrier.UserPrefs.MutedChannels
	if raw == "" {
		return nil, nil
	}
	ids := strings.Split(raw, ",")
	muted := make(map[string]bool, len(ids))
	for _, id := range ids {
		muted[id] = true
	}
	return muted, nil
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
