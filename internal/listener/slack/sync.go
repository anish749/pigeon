package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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

// Sync fetches historical messages for conversations the user was active in
// during the last 30 days. On first run, syncs the last 90 days. On subsequent
// runs, picks up from where it left off using stored cursors per channel.
// Uses the user token to access DMs and all user-visible conversations.
func Sync(ctx context.Context, userToken string, resolver *Resolver, workspace string) error {
	api := goslack.New(userToken)
	gate := &rateLimitGate{workspace: workspace}
	cursors := loadCursors(workspace)
	activityCutoff := time.Now().AddDate(0, 0, -activityDays)
	defaultOldest := fmt.Sprintf("%d", time.Now().AddDate(0, 0, -syncDays).Unix())

	conversations, err := listUserConversations(ctx, api, gate)
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}

	// Sort: DMs first, then group IMs, then private channels, then public channels
	sort.SliceStable(conversations, func(i, j int) bool {
		return channelPriority(conversations[i]) < channelPriority(conversations[j])
	})

	slog.InfoContext(ctx, "slack sync: found user conversations",
		"total", len(conversations), "workspace", workspace)

	// Register all channel names in resolver so real-time listener knows about them
	for _, ch := range conversations {
		resolver.RegisterConversation(ctx, ch)
	}

	var synced, totalMessages int
	for _, ch := range conversations {
		if ctx.Err() != nil {
			saveCursors(workspace, cursors)
			return ctx.Err()
		}

		// If we have a cursor, resume from there. Otherwise, probe the last
		// 30 days first — if empty, skip. If active, go back the full 90 days.
		// This saves a full history fetch for every inactive channel at the
		// cost of one cheap call that returns at most one page.
		oldest := defaultOldest
		if cursor, ok := cursors[ch.ID]; ok {
			oldest = cursor
		} else {
			probe, err := fetchHistory(ctx, api, gate, ch.ID, fmt.Sprintf("%d", activityCutoff.Unix()))
			if err != nil {
				slog.WarnContext(ctx, "slack sync: probe failed",
					"channel", ch.ID, "error", err)
				continue
			}
			if len(probe) == 0 {
				continue
			}
		}

		channelName := resolver.ChannelName(ctx, ch.ID)

		msgs, err := fetchHistory(ctx, api, gate, ch.ID, oldest)
		if err != nil {
			slog.WarnContext(ctx, "slack sync: fetch failed",
				"channel", channelName, "error", err)
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
			ts := ParseTimestamp(msg.Timestamp)

			if err := store.WriteMessage("slack", workspace, channelName, userName, msg.Text, ts); err != nil {
				slog.WarnContext(ctx, "slack sync: write failed", "error", err)
				continue
			}
			written++
		}

		if lastTS != "" {
			cursors[ch.ID] = lastTS
		}

		if written > 0 {
			synced++
			totalMessages += written
			slog.InfoContext(ctx, "slack sync: channel done",
				"channel", channelName, "messages", written, "workspace", workspace)
		}
	}

	if err := saveCursors(workspace, cursors); err != nil {
		slog.WarnContext(ctx, "slack sync: failed to save cursors", "error", err)
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

// fetchHistory retrieves all messages in a channel since the given Slack timestamp,
// returned in chronological order. The oldest parameter is exclusive (messages
// strictly after it are returned).
func fetchHistory(ctx context.Context, api *goslack.Client, gate *rateLimitGate, channelID string, oldest string) ([]goslack.Message, error) {
	var all []goslack.Message
	cursor := ""

	for {
		if err := gate.wait(ctx); err != nil {
			return all, err
		}

		resp, err := api.GetConversationHistoryContext(ctx, &goslack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Limit:     200,
			Cursor:    cursor,
		})
		if gate.update(err) {
			continue
		}
		if err != nil {
			return all, err
		}
		all = append(all, resp.Messages...)
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
