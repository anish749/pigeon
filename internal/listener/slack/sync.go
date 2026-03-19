package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	gosync "sync"
	"time"

	goslack "github.com/slack-go/slack"
	"gopkg.in/yaml.v3"

	"github.com/anish/claude-msg-utils/internal/store"
)

const (
	syncDays     = 90
	activityDays = 30
)

// syncCursors maps channel ID to the last synced Slack message timestamp.
// Stored as .sync-cursors.yaml in the workspace data directory.
type syncCursors map[string]string

func cursorsPath(workspace string) string {
	return filepath.Join(store.DataDir(), "slack", workspace, ".sync-cursors.yaml")
}

func loadCursors(workspace string) syncCursors {
	data, err := os.ReadFile(cursorsPath(workspace))
	if err != nil {
		return make(syncCursors)
	}
	var c syncCursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return make(syncCursors)
	}
	return c
}

func saveCursors(workspace string, c syncCursors) error {
	path := cursorsPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// MessageStore is the single write path for Slack messages. It writes message
// files and maintains per-channel cursors so that both sync and the real-time
// listener stay consistent.
type MessageStore struct {
	workspace string
	mu        gosync.Mutex
	cursors   syncCursors
}

// NewMessageStore creates a MessageStore, loading any existing cursors from disk.
func NewMessageStore(workspace string) *MessageStore {
	return &MessageStore{
		workspace: workspace,
		cursors:   loadCursors(workspace),
	}
}

// Write persists a message to the appropriate date file. Does not advance the
// cursor — only sync should do that via AdvanceCursor.
func (ms *MessageStore) Write(channelID, channelName, sender, text string, ts time.Time, slackTS string) error {
	return store.WriteMessage("slack", ms.workspace, channelName, sender, text, ts)
}

// AdvanceCursor updates the cursor without writing a message (e.g. for skipped bot messages).
func (ms *MessageStore) AdvanceCursor(channelID, slackTS string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.cursors[channelID] = slackTS
	saveCursors(ms.workspace, ms.cursors)
}

// Cursor returns the stored cursor for a channel.
func (ms *MessageStore) Cursor(channelID string) (string, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	v, ok := ms.cursors[channelID]
	return v, ok
}

// Sync fetches historical messages for conversations the user was active in
// during the last 30 days. On first run, syncs the last 90 days. On subsequent
// runs, picks up from where it left off using stored cursors per channel.
// Uses the user token to access DMs and all user-visible conversations.
func Sync(ctx context.Context, userToken string, resolver *Resolver, workspace string, ms *MessageStore) error {
	api := goslack.New(userToken)
	gate := &rateLimitGate{workspace: workspace}
	activityCutoff := time.Now().AddDate(0, 0, -activityDays)
	defaultOldest := fmt.Sprintf("%d", time.Now().AddDate(0, 0, -syncDays).Unix())

	allConversations, err := listUserConversations(ctx, api, gate)
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}

	// Filter out public channels the user hasn't joined and Slackbot
	var conversations []goslack.Channel
	var skippedPublic int
	for _, ch := range allConversations {
		if !ch.IsIM && !ch.IsMpIM && !ch.IsPrivate && !ch.IsMember {
			skippedPublic++
			continue
		}
		if ch.IsIM && ch.User == "USLACKBOT" {
			continue
		}
		conversations = append(conversations, ch)
	}

	// Count by type after filtering
	var totalDMs, totalMpIMs, totalPrivate, totalPublic int
	for _, ch := range conversations {
		switch channelPriority(ch) {
		case 0:
			totalDMs++
		case 1:
			totalMpIMs++
		case 2:
			totalPrivate++
		case 3:
			totalPublic++
		}
	}

	// Sort: DMs first, then group IMs, then private channels, then public channels
	sort.SliceStable(conversations, func(i, j int) bool {
		return channelPriority(conversations[i]) < channelPriority(conversations[j])
	})

	slog.InfoContext(ctx, "slack sync: conversations",
		"workspace", workspace,
		"dms", totalDMs,
		"group_ims", totalMpIMs,
		"private", totalPrivate,
		"public", totalPublic,
		"skipped_non_member", skippedPublic,
		"total", len(conversations),
	)

	// Register all channel names in resolver so real-time listener knows about them
	for _, ch := range conversations {
		resolver.RegisterConversation(ctx, ch)
	}

	// Track per-category progress
	var doneDMs, doneMpIMs, donePrivate, donePublic int

	var synced, totalMessages int
	for _, ch := range conversations {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		gate.channel = resolver.ChannelName(ctx, ch.ID)
		gate.progress = fmt.Sprintf("dms: %d/%d | group_ims: %d/%d | private: %d/%d | public: %d/%d",
			doneDMs, totalDMs, doneMpIMs, totalMpIMs, donePrivate, totalPrivate, donePublic, totalPublic)

		// Use cursor if resuming, otherwise go back 90 days.
		// The first page of fetchHistory doubles as the activity check —
		// if it comes back empty, we move on. No separate probe call.
		oldest := defaultOldest
		if cursor, ok := ms.Cursor(ch.ID); ok {
			oldest = cursor
		}

		channelName := resolver.ChannelName(ctx, ch.ID)

		msgs, err := fetchHistory(ctx, api, gate, ch.ID, oldest, activityCutoff)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "channel_not_found") || strings.Contains(errStr, "is_archived") {
				ms.AdvanceCursor(ch.ID, oldest)
			} else {
				slog.WarnContext(ctx, "slack sync: fetch failed",
					"channel", channelName, "error", err)
			}
			continue
		}

		var lastTS string
		written := 0
		for _, msg := range msgs {
			// Track the latest timestamp regardless of whether we write the message
			lastTS = msg.Timestamp

			if msg.BotID != "" || msg.SubType != "" || msg.Text == "" {
				continue
			}

			userName := resolver.UserName(ctx, msg.User)
			text := resolver.ResolveText(ctx, msg.Text)
			ts := ParseTimestamp(msg.Timestamp)

			if err := ms.Write(ch.ID, channelName, userName, text, ts, msg.Timestamp); err != nil {
				slog.WarnContext(ctx, "slack sync: write failed", "error", err)
				continue
			}
			written++
		}

		if lastTS != "" {
			ms.AdvanceCursor(ch.ID, lastTS)
		} else if _, hasCursor := ms.Cursor(ch.ID); !hasCursor {
			// Mark empty channels so we don't re-probe them on next restart.
			// Use the current oldest as a sentinel — next run will resume from here
			// and immediately get an empty response.
			ms.AdvanceCursor(ch.ID, oldest)
		}

		if written > 0 {
			synced++
			totalMessages += written
			slog.InfoContext(ctx, "slack sync: channel done",
				"channel", channelName, "messages", written, "workspace", workspace)
		}

		switch channelPriority(ch) {
		case 0:
			doneDMs++
		case 1:
			doneMpIMs++
		case 2:
			donePrivate++
		case 3:
			donePublic++
		}
	}

	slog.InfoContext(ctx, "slack sync: complete",
		"workspace", workspace, "channels", synced, "messages", totalMessages)
	return nil
}

// listUserConversations paginates through all conversations visible to the user token.
func listUserConversations(ctx context.Context, api *goslack.Client, gate *rateLimitGate) ([]goslack.Channel, error) {
	var all []goslack.Channel
	cursor := ""
	for {
		if err := gate.wait(ctx); err != nil {
			return all, err
		}

		params := &goslack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			ExcludeArchived: true,
			Limit:           1000,
			Cursor:          cursor,
		}
		channels, nextCursor, err := api.GetConversationsContext(ctx, params)
		if gate.update(err) {
			continue // retry after wait
		}
		if err != nil {
			return all, err
		}
		all = append(all, channels...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return all, nil
}

// fetchHistory retrieves messages in a channel since the given Slack timestamp,
// returned in chronological order. The oldest parameter is exclusive (messages
// strictly after it are returned).
//
// activityCutoff controls pagination: after the first page, if the newest message
// in that page is older than activityCutoff, pagination stops. This avoids
// fetching the full 90-day history for channels with no recent activity.
// Pass a zero time to always fetch everything.
func fetchHistory(ctx context.Context, api *goslack.Client, gate *rateLimitGate, channelID string, oldest string, activityCutoff time.Time) ([]goslack.Message, error) {
	var all []goslack.Message
	cursor := ""
	firstPage := true

	for {
		if err := gate.wait(ctx); err != nil {
			return all, err
		}

		resp, err := api.GetConversationHistoryContext(ctx, &goslack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Limit:     1000,
			Cursor:    cursor,
		})
		if gate.update(err) {
			continue
		}
		if err != nil {
			return all, err
		}
		all = append(all, resp.Messages...)

		// After the first page, check if the channel has recent activity.
		// Slack returns newest-first, so Messages[0] is the most recent.
		// If it's older than the cutoff, don't bother paginating further.
		if firstPage && !activityCutoff.IsZero() && len(resp.Messages) > 0 {
			newest := ParseTimestamp(resp.Messages[0].Timestamp)
			if newest.Before(activityCutoff) {
				break
			}
		}
		firstPage = false

		if !resp.HasMore {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
	}

	// Reverse: API returns newest-first, we want chronological order
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
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
