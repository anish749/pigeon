package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"time"

	goslack "github.com/slack-go/slack"
	"gopkg.in/yaml.v3"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/syncstatus"
)

const (
	syncDays     = 90
	activityDays = 30
)

// syncCursors maps channel ID to the last synced Slack message timestamp.
// Stored as .sync-cursors.yaml in the workspace data directory.
type syncCursors map[string]string

func cursorsPath(acct account.Account) string {
	return paths.DefaultDataRoot().AccountFor(acct).SyncCursorsPath()
}

func loadCursors(acct account.Account) syncCursors {
	data, err := os.ReadFile(cursorsPath(acct))
	if err != nil {
		return make(syncCursors)
	}
	var c syncCursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return make(syncCursors)
	}
	// yaml.Unmarshal on an empty file succeeds but leaves the map nil.
	// This happens after a reset clears message data but leaves an empty cursor file.
	if c == nil {
		return make(syncCursors)
	}
	return c
}

func saveCursors(acct account.Account, c syncCursors) error {
	path := cursorsPath(acct)
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
	acct    account.Account
	store   store.Store
	mu      gosync.Mutex
	cursors syncCursors
}

// NewMessageStore creates a MessageStore, loading any existing cursors from disk.
func NewMessageStore(acct account.Account, s store.Store) *MessageStore {
	return &MessageStore{
		acct:    acct,
		store:   s,
		cursors: loadCursors(acct),
	}
}

// Append stores any line type in the date file for its timestamp.
func (ms *MessageStore) Append(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendThread stores any line type in the given thread file.
func (ms *MessageStore) AppendThread(channelName, threadTS string, line modelv1.Line) error {
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// EnsureThreadContextSeparator writes the separator line to a thread file.
func (ms *MessageStore) EnsureThreadContextSeparator(channelName, threadTS string) error {
	line := modelv1.Line{Type: modelv1.LineSeparator}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// ThreadExists checks if a thread file exists for the given thread timestamp.
func (ms *MessageStore) ThreadExists(channelName, threadTS string) bool {
	return ms.store.ThreadExists(ms.acct, channelName, threadTS)
}

// EnsureMeta writes .meta.json for a conversation if it doesn't already exist.
func (ms *MessageStore) EnsureMeta(conversation string, meta modelv1.ConvMeta) {
	if _, err := ms.store.WriteMetaIfNotExists(ms.acct, conversation, meta); err != nil {
		slog.Warn("write meta failed", "conversation", conversation, "error", err)
	}
}

// AdvanceCursor updates the cursor without writing a message (e.g. for skipped bot messages).
func (ms *MessageStore) AdvanceCursor(channelID, slackTS string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.cursors[channelID] = slackTS
	saveCursors(ms.acct, ms.cursors)
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
// Syncs both user conversations (via user token) and bot DM conversations
// (via bot token), interleaving them into the same contact directories.
func Sync(ctx context.Context, userToken, botToken string, resolver *Resolver, acct account.Account, ms *MessageStore, tracker *syncstatus.Tracker) (retErr error) {
	statusKey := acct.Display()
	tracker.Start(statusKey, syncstatus.KindBackfill)
	defer func() { tracker.Done(statusKey, retErr) }()

	api := goslack.New(userToken)
	gate := &rateLimitGate{workspace: acct.Name}
	activityCutoff := time.Now().AddDate(0, 0, -activityDays)
	defaultOldest := fmt.Sprintf("%d", time.Now().AddDate(0, 0, -syncDays).Unix())

	allConversations, err := listUserConversations(ctx, api, gate)
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}

	// Filter out public channels the user hasn't joined and Slackbot.
	var memberConversations []goslack.Channel
	var skippedPublic int
	for _, ch := range allConversations {
		if !ch.IsIM && !ch.IsMpIM && !ch.IsPrivate && !ch.IsMember {
			skippedPublic++
			continue
		}
		if ch.IsIM && ch.User == "USLACKBOT" {
			continue
		}
		memberConversations = append(memberConversations, ch)
	}

	// Register all channel names and membership in resolver so the real-time
	// listener knows about them. This must happen for all member conversations,
	// not just the ones selected for sync.
	for _, ch := range memberConversations {
		if err := resolver.RegisterConversation(ctx, ch); err != nil {
			slog.WarnContext(ctx, "slack sync: failed to register conversation",
				"channel", ch.ID, "error", err)
		}
		resolver.AddMember(ch.ID)
		channelName, err := resolver.ChannelName(ctx, ch.ID)
		if err != nil {
			slog.WarnContext(ctx, "slack sync: cannot resolve channel name for meta",
				"channel_id", ch.ID, "error", err)
			continue
		}
		ms.EnsureMeta(channelName, resolver.ConvMeta(ch.ID, channelName))
	}

	// Determine which channels need syncing. Returns a sorted, filtered list:
	// only channels with recent activity (or all channels on first sync / small workspaces).
	// Muted channels are excluded — they still get registered in the resolver
	// above so real-time messages work, but we don't spend sync budget on them.
	conversations := prioritizeChannels(ctx, api, gate, ms.cursors, memberConversations)

	// Count by type for progress reporting.
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

	slog.InfoContext(ctx, "slack sync: conversations",
		"account", acct,
		"dms", totalDMs,
		"group_ims", totalMpIMs,
		"private", totalPrivate,
		"public", totalPublic,
		"skipped_non_member", skippedPublic,
		"total_member", len(memberConversations),
		"to_sync", len(conversations),
	)

	// Track per-category progress
	var doneDMs, doneMpIMs, donePrivate, donePublic int
	var synced, totalMessages int
	for _, ch := range conversations {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		channelName, err := resolver.ChannelName(ctx, ch.ID)
		if err != nil {
			slog.WarnContext(ctx, "slack sync: cannot resolve channel name",
				"channel_id", ch.ID, "error", err)
			continue
		}
		gate.channel = channelName
		gate.progress = fmt.Sprintf("dms: %d/%d | group_ims: %d/%d | private: %d/%d | public: %d/%d",
			doneDMs, totalDMs, doneMpIMs, totalMpIMs, donePrivate, totalPrivate, donePublic, totalPublic)
		tracker.Update(statusKey, gate.progress)

		// Use cursor if resuming, otherwise go back 90 days.
		oldest := defaultOldest
		if cursor, ok := ms.Cursor(ch.ID); ok {
			oldest = cursor
		}

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

			// Skip system events (channel_join, channel_topic, etc.).
			// Allow bot messages through — they contain valuable info
			// (alerts, CI, integrations).
			if !allowedSubType(msg.SubType) {
				continue
			}

			hasBlocks := len(msg.Blocks.BlockSet) > 0 || len(msg.Attachments) > 0
			if msg.Text == "" && !hasBlocks {
				continue
			}

			userName, userID, err := resolver.SenderName(ctx, msg.User, msg.BotID, msg.Username)
			if err != nil {
				slog.WarnContext(ctx, "slack sync: skipping message, cannot resolve sender",
					"channel", channelName, "ts", msg.Timestamp, "error", err)
				continue
			}
			ts := ParseTimestamp(msg.Timestamp)

			var line modelv1.Line
			if msg.Text != "" {
				text, err := resolver.ResolveText(ctx, msg.Text)
				if err != nil {
					slog.WarnContext(ctx, "slack sync: skipping message, cannot resolve text",
						"channel", channelName, "ts", msg.Timestamp, "error", err)
					continue
				}
				line = modelv1.NewMsgLine(msg.Timestamp, ts, userName, userID, text, modelv1.ViaOrganic, false)
			} else {
				var err error
				line, err = modelv1.NewSlackBlockLine(slackBlockPayload(msg.Timestamp, ts, userName, userID, modelv1.ViaOrganic, false, msg.Blocks, msg.Attachments))
				if err != nil {
					slog.WarnContext(ctx, "slack sync: build block line", "error", err)
					continue
				}
			}

			if err := ms.Append(channelName, line); err != nil {
				slog.WarnContext(ctx, "slack sync: write failed", "error", err)
				continue
			}
			written++
			if err := writeReactions(ctx, ms, resolver, channelName, msg); err != nil {
				slog.WarnContext(ctx, "slack sync: reaction write failed", "error", err)
			}
		}

		// Sync thread replies for messages with threads
		threadsSynced := syncThreads(ctx, api, gate, resolver, ms, ch.ID, channelName, msgs)

		if lastTS != "" {
			ms.AdvanceCursor(ch.ID, lastTS)
		} else if _, hasCursor := ms.Cursor(ch.ID); !hasCursor {
			// Mark empty channels so we don't re-probe them on next restart.
			// Use the current oldest as a sentinel — next run will resume from here
			// and immediately get an empty response.
			ms.AdvanceCursor(ch.ID, oldest)
		}

		if len(msgs) > 0 && written == 0 && threadsSynced == 0 {
			slog.ErrorContext(ctx, "slack sync: all messages filtered",
				"channel", channelName, "fetched", len(msgs), "account", acct)
		}

		if written > 0 || threadsSynced > 0 {
			synced++
			totalMessages += written
			slog.InfoContext(ctx, "slack sync: channel done",
				"channel", channelName, "messages", written,
				"threads", threadsSynced, "account", acct)
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
		"account", acct, "channels", synced, "messages", totalMessages)

	// Sync bot DM conversations so pigeon messages appear in the unified timeline.
	if err := syncBotDMs(ctx, botToken, resolver, acct, ms, gate, defaultOldest, activityCutoff); err != nil {
		slog.ErrorContext(ctx, "slack sync: bot DM sync failed", "account", acct, "error", err)
	}

	// Run maintenance after sync. Sync writes user messages and bot DM messages
	// to the same date files, potentially out of order and with duplicates.
	// Maintenance deduplicates, sorts, and compacts these files on disk.
	//
	// This is best-effort: if it fails, correctness is not affected because
	// readers always dedup and sort in-memory. The periodic maintenance pass
	// will also pick up any files missed here. We run it eagerly after sync
	// because we know the files are dirty and it improves on-disk readability
	// for grep/cat.
	if err := ms.store.Maintain(acct); err != nil {
		slog.WarnContext(ctx, "slack sync: maintenance failed", "account", acct, "error", err)
	}

	return nil
}

// syncBotDMs syncs the bot's DM conversations. Messages are written to the same
// contact directory as the user's DMs, with "sent to pigeon by" / "sent by pigeon"
// labels so they interleave in the unified timeline.
func syncBotDMs(ctx context.Context, botToken string, resolver *Resolver, acct account.Account, ms *MessageStore, gate *rateLimitGate, defaultOldest string, activityCutoff time.Time) error {
	botAPI := goslack.New(botToken)

	// List bot's DM and group DM conversations only.
	var botDMs []goslack.Channel
	cursor := ""
	for {
		if err := gate.wait(ctx); err != nil {
			return err
		}
		params := &goslack.GetConversationsParameters{
			Types:           []string{"im", "mpim"},
			ExcludeArchived: true,
			Limit:           1000,
			Cursor:          cursor,
		}
		channels, nextCursor, err := botAPI.GetConversationsContext(ctx, params)
		if gate.update(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("list bot conversations: %w", err)
		}
		botDMs = append(botDMs, channels...)
		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	if len(botDMs) == 0 {
		return nil
	}

	slog.InfoContext(ctx, "slack sync: bot DMs", "account", acct, "count", len(botDMs))

	for _, ch := range botDMs {
		if err := resolver.RegisterConversation(ctx, ch); err != nil {
			slog.WarnContext(ctx, "slack sync: failed to register bot DM",
				"channel", ch.ID, "error", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Resolve channel name to "@Username" so bot DMs are stored in the
		// same directory as the user's DMs with that contact.
		var channelName string
		if ch.IsIM {
			userName, err := resolver.UserName(ctx, ch.User)
			if err != nil {
				slog.WarnContext(ctx, "slack sync: skipping bot DM, cannot resolve user",
					"channel", ch.ID, "user", ch.User, "error", err)
				continue
			}
			channelName = "@" + userName
		} else {
			channelName = FormatChannelName(ch)
		}
		ms.EnsureMeta(channelName, resolver.ConvMeta(ch.ID, channelName))

		oldest := defaultOldest
		if c, ok := ms.Cursor(ch.ID); ok {
			oldest = c
		}

		msgs, err := fetchHistory(ctx, botAPI, gate, ch.ID, oldest, activityCutoff)
		if err != nil {
			slog.WarnContext(ctx, "slack sync: bot DM fetch failed",
				"channel", channelName, "error", err)
			continue
		}

		var lastTS string
		written := 0
		for _, msg := range msgs {
			lastTS = msg.Timestamp

			if msg.SubType != "" && msg.SubType != "thread_broadcast" {
				continue
			}

			hasBlocks := len(msg.Blocks.BlockSet) > 0 || len(msg.Attachments) > 0
			if msg.Text == "" && !hasBlocks {
				continue
			}

			ts := ParseTimestamp(msg.Timestamp)

			var senderName string
			var senderID string
			var via modelv1.Via
			if msg.BotID != "" {
				senderName = "sent by pigeon"
				senderID = msg.BotID
				via = modelv1.ViaPigeonAsBot
			} else {
				userName, err := resolver.UserName(ctx, msg.User)
				if err != nil {
					slog.WarnContext(ctx, "slack sync: skipping bot DM message, cannot resolve user",
						"channel", channelName, "ts", msg.Timestamp, "error", err)
					continue
				}
				senderName = "sent to pigeon by " + userName
				senderID = msg.User
				via = modelv1.ViaToPigeon
			}

			var line modelv1.Line
			if msg.Text != "" {
				text, err := resolver.ResolveText(ctx, msg.Text)
				if err != nil {
					slog.WarnContext(ctx, "slack sync: skipping bot DM message, cannot resolve text",
						"channel", channelName, "ts", msg.Timestamp, "error", err)
					continue
				}
				line = modelv1.NewMsgLine(msg.Timestamp, ts, senderName, senderID, text, via, false)
			} else {
				var err error
				line, err = modelv1.NewSlackBlockLine(slackBlockPayload(msg.Timestamp, ts, senderName, senderID, via, false, msg.Blocks, msg.Attachments))
				if err != nil {
					slog.WarnContext(ctx, "slack sync: build block line", "error", err)
					continue
				}
			}

			if err := ms.Append(channelName, line); err != nil {
				slog.WarnContext(ctx, "slack sync: bot DM write failed", "error", err)
				continue
			}
			written++
			if err := writeReactions(ctx, ms, resolver, channelName, msg); err != nil {
				slog.WarnContext(ctx, "slack sync: bot DM reaction write failed", "error", err)
			}
		}

		if lastTS != "" {
			ms.AdvanceCursor(ch.ID, lastTS)
		} else if _, hasCursor := ms.Cursor(ch.ID); !hasCursor {
			ms.AdvanceCursor(ch.ID, oldest)
		}

		if written > 0 {
			slog.InfoContext(ctx, "slack sync: bot DM done",
				"channel", channelName, "messages", written, "account", acct)
		}
	}

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

// contextMessages is the number of channel messages after a thread parent to
// include as surrounding context in the thread file.
const contextMessages = 10

// syncThreads fetches thread replies for messages with ReplyCount > 0,
// writing them to separate thread files along with surrounding channel context.
// Returns the number of threads synced.
func syncThreads(ctx context.Context, api *goslack.Client, gate *rateLimitGate, resolver *Resolver, ms *MessageStore, channelID, channelName string, msgs []goslack.Message) int {
	// Build index from timestamp to position for channel context lookup
	msgIndex := make(map[string]int, len(msgs))
	for i, msg := range msgs {
		msgIndex[msg.Timestamp] = i
	}

	var synced int
	for _, msg := range msgs {
		if ctx.Err() != nil {
			break
		}
		if msg.ReplyCount == 0 {
			continue
		}

		replies, err := fetchThreadReplies(ctx, api, gate, channelID, msg.Timestamp)
		if err != nil {
			slog.WarnContext(ctx, "slack sync: thread fetch failed",
				"channel", channelName, "thread_ts", msg.Timestamp, "error", err)
			continue
		}

		// Write parent message (first reply from conversations.replies is the parent)
		// Then write each reply indented
		for _, reply := range replies {
			if !allowedSubType(reply.SubType) {
				continue
			}
			hasBlocks := len(reply.Blocks.BlockSet) > 0 || len(reply.Attachments) > 0
			if reply.Text == "" && !hasBlocks {
				continue
			}
			userName, userID, err := resolver.SenderName(ctx, reply.User, reply.BotID, reply.Username)
			if err != nil {
				slog.WarnContext(ctx, "slack sync: skipping thread reply, cannot resolve sender",
					"channel", channelName, "thread_ts", msg.Timestamp, "ts", reply.Timestamp, "error", err)
				continue
			}
			ts := ParseTimestamp(reply.Timestamp)
			isReply := reply.Timestamp != msg.Timestamp // parent vs reply

			var line modelv1.Line
			if reply.Text != "" {
				text, err := resolver.ResolveText(ctx, reply.Text)
				if err != nil {
					slog.WarnContext(ctx, "slack sync: skipping thread reply, cannot resolve text",
						"channel", channelName, "thread_ts", msg.Timestamp, "ts", reply.Timestamp, "error", err)
					continue
				}
				line = modelv1.NewMsgLine(reply.Timestamp, ts, userName, userID, text, modelv1.ViaOrganic, isReply)
			} else {
				var err error
				line, err = modelv1.NewSlackBlockLine(slackBlockPayload(reply.Timestamp, ts, userName, userID, modelv1.ViaOrganic, isReply, reply.Blocks, reply.Attachments))
				if err != nil {
					slog.WarnContext(ctx, "slack sync: build block line", "error", err)
					continue
				}
			}
			if err := ms.AppendThread(channelName, msg.Timestamp, line); err != nil {
				slog.WarnContext(ctx, "slack sync: thread write failed", "error", err)
			}
			if err := writeReactions(ctx, ms, resolver, channelName, reply); err != nil {
				slog.WarnContext(ctx, "slack sync: thread reaction write failed", "error", err)
			}
		}

		// Write surrounding channel context: the next N messages after the parent
		if idx, ok := msgIndex[msg.Timestamp]; ok {
			contextStart := idx + 1
			contextEnd := contextStart + contextMessages
			if contextEnd > len(msgs) {
				contextEnd = len(msgs)
			}
			if contextStart < contextEnd {
				if err := ms.EnsureThreadContextSeparator(channelName, msg.Timestamp); err != nil {
					slog.WarnContext(ctx, "slack sync: thread context separator failed", "error", err)
				}
				for _, ctxMsg := range msgs[contextStart:contextEnd] {
					// Stop at the next thread parent to avoid crossing topic boundaries
					if ctxMsg.ReplyCount > 0 {
						break
					}
					if !allowedSubType(ctxMsg.SubType) {
						continue
					}
					hasBlocks := len(ctxMsg.Blocks.BlockSet) > 0 || len(ctxMsg.Attachments) > 0
					if ctxMsg.Text == "" && !hasBlocks {
						continue
					}
					userName, userID, err := resolver.SenderName(ctx, ctxMsg.User, ctxMsg.BotID, ctxMsg.Username)
					if err != nil {
						slog.WarnContext(ctx, "slack sync: skipping thread context msg, cannot resolve sender",
							"channel", channelName, "thread_ts", msg.Timestamp, "ts", ctxMsg.Timestamp, "error", err)
						continue
					}
					ts := ParseTimestamp(ctxMsg.Timestamp)

					var line modelv1.Line
					if ctxMsg.Text != "" {
						text, err := resolver.ResolveText(ctx, ctxMsg.Text)
						if err != nil {
							slog.WarnContext(ctx, "slack sync: skipping thread context msg, cannot resolve text",
								"channel", channelName, "thread_ts", msg.Timestamp, "ts", ctxMsg.Timestamp, "error", err)
							continue
						}
						line = modelv1.NewMsgLine(ctxMsg.Timestamp, ts, userName, userID, text, modelv1.ViaOrganic, false)
					} else {
						var err error
						line, err = modelv1.NewSlackBlockLine(slackBlockPayload(ctxMsg.Timestamp, ts, userName, userID, modelv1.ViaOrganic, false, ctxMsg.Blocks, ctxMsg.Attachments))
						if err != nil {
							slog.WarnContext(ctx, "slack sync: build block line", "error", err)
							continue
						}
					}
					if err := ms.AppendThread(channelName, msg.Timestamp, line); err != nil {
						slog.WarnContext(ctx, "slack sync: thread context write failed", "error", err)
					}
				}
			}
		}

		synced++
	}
	return synced
}

// fetchThreadReplies retrieves all replies in a thread, returned in chronological order.
// The first message in the result is the thread parent (conversations.replies always includes it).
func fetchThreadReplies(ctx context.Context, api *goslack.Client, gate *rateLimitGate, channelID, threadTS string) ([]goslack.Message, error) {
	var all []goslack.Message
	cursor := ""
	for {
		if err := gate.wait(ctx); err != nil {
			return all, err
		}
		msgs, hasMore, nextCursor, err := api.GetConversationRepliesContext(ctx, &goslack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Limit:     1000,
			Cursor:    cursor,
		})
		if gate.update(err) {
			continue
		}
		if err != nil {
			return all, err
		}
		all = append(all, msgs...)
		if !hasMore {
			break
		}
		cursor = nextCursor
	}
	return all, nil
}

