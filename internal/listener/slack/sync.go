package slack

import (
	"context"
	"errors"
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
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store"
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

// AppendReaction stores a reaction or unreaction event in the date file
// corresponding to the target message's timestamp.
func (ms *MessageStore) AppendReaction(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendEdit stores a message edit event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendEdit(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// AppendDelete stores a message delete event in the date file corresponding
// to the target message's timestamp.
func (ms *MessageStore) AppendDelete(channelName string, line modelv1.Line) error {
	return ms.store.Append(ms.acct, channelName, line)
}

// Write persists a message to the appropriate date file. Does not advance the
// cursor — only sync should do that via AdvanceCursor.
func (ms *MessageStore) Write(channelID, channelName, sender, senderID, text string, ts time.Time, slackTS string, via modelv1.Via) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Via:      via,
			Text:     text,
		},
	}
	return ms.store.Append(ms.acct, channelName, line)
}

// WriteThreadMessage writes a message to a thread file.
func (ms *MessageStore) WriteThreadMessage(channelName, threadTS, sender, senderID, text string, ts time.Time, slackTS string, isReply bool, via modelv1.Via) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Via:      via,
			Text:     text,
			Reply:    isReply,
		},
	}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// WriteThreadContext writes a channel context message to a thread file.
func (ms *MessageStore) WriteThreadContext(channelName, threadTS, sender, senderID, text string, ts time.Time, slackTS string) error {
	line := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID:       slackTS,
			Ts:       ts,
			Sender:   sender,
			SenderID: senderID,
			Text:     text,
		},
	}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// EnsureThreadContextSeparator writes the separator line to a thread file.
func (ms *MessageStore) EnsureThreadContextSeparator(channelName, threadTS string) error {
	line := modelv1.Line{Type: modelv1.LineSeparator}
	return ms.store.AppendThread(ms.acct, channelName, threadTS, line)
}

// syncMessage filters, resolves, writes a Slack message, and syncs its reactions.
// Returns true if the message was written (false if filtered out).
// Skips bot messages, system events (except thread_broadcast), and empty text.
// The write function is called with the resolved fields; its signature varies
// by target (channel date file vs thread file).
func syncMessage(ctx context.Context, ms *MessageStore, resolver *Resolver, channelName string, msg goslack.Message, write func(sender, senderID, text string, ts time.Time) error) bool {
	if msg.BotID != "" || (msg.SubType != "" && msg.SubType != "thread_broadcast") || msg.Text == "" {
		return false
	}

	userName := resolver.UserName(ctx, msg.User)
	text := resolver.ResolveText(ctx, msg.Text)
	ts := ParseTimestamp(msg.Timestamp)

	if err := write(userName, msg.User, text, ts); err != nil {
		slog.WarnContext(ctx, "slack sync: write failed", "error", err)
		return false
	}

	syncReactions(ctx, ms, resolver, channelName, msg)
	return true
}

// syncReactions writes LineReaction events for each reaction on a Slack message.
// Slack groups reactions by emoji with a user list; this expands them into
// one LineReaction per user per emoji. Deduplication is handled by compaction.
func syncReactions(ctx context.Context, ms *MessageStore, resolver *Resolver, channelName string, msg goslack.Message) {
	if len(msg.Reactions) == 0 {
		return
	}
	var errs []error
	for _, reaction := range msg.Reactions {
		for _, userID := range reaction.Users {
			line := modelv1.Line{
				Type: modelv1.LineReaction,
				React: &modelv1.ReactLine{
					Ts:       ParseTimestamp(msg.Timestamp),
					MsgID:    msg.Timestamp,
					Sender:   resolver.UserName(ctx, userID),
					SenderID: userID,
					Emoji:    reaction.Name,
				},
			}
			if err := ms.AppendReaction(channelName, line); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		slog.WarnContext(ctx, "slack sync: reaction write failed", "error", err)
	}
}

// ThreadExists checks if a thread file exists for the given thread timestamp.
func (ms *MessageStore) ThreadExists(channelName, threadTS string) bool {
	return ms.store.ThreadExists(ms.acct, channelName, threadTS)
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
func Sync(ctx context.Context, userToken, botToken string, resolver *Resolver, acct account.Account, ms *MessageStore) error {
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
		resolver.RegisterConversation(ctx, ch)
		resolver.AddMember(ch.ID)
	}

	// Determine which channels need syncing. Returns a sorted, filtered list:
	// only channels with recent activity (or all channels on first sync / small workspaces).
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

		gate.channel = resolver.ChannelName(ctx, ch.ID)
		gate.progress = fmt.Sprintf("dms: %d/%d | group_ims: %d/%d | private: %d/%d | public: %d/%d",
			doneDMs, totalDMs, doneMpIMs, totalMpIMs, donePrivate, totalPrivate, donePublic, totalPublic)

		// Use cursor if resuming, otherwise go back 90 days.
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
			// Track the latest timestamp regardless of whether we write the message.
			lastTS = msg.Timestamp

			if syncMessage(ctx, ms, resolver, channelName, msg, func(sender, senderID, text string, ts time.Time) error {
				return ms.Write(ch.ID, channelName, sender, senderID, text, ts, msg.Timestamp, modelv1.ViaOrganic)
			}) {
				written++
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
		resolver.RegisterConversation(ctx, ch)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Resolve channel name to "@Username" so bot DMs are stored in the
		// same directory as the user's DMs with that contact.
		var channelName string
		if ch.IsIM {
			channelName = "@" + resolver.UserName(ctx, ch.User)
		} else {
			channelName = FormatChannelName(ch)
		}

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

			if (msg.SubType != "" && msg.SubType != "thread_broadcast") || msg.Text == "" {
				continue
			}

			text := resolver.ResolveText(ctx, msg.Text)
			ts := ParseTimestamp(msg.Timestamp)

			var senderName string
			var senderID string
			var via modelv1.Via
			if msg.BotID != "" {
				senderName = "sent by pigeon"
				senderID = msg.BotID
				via = modelv1.ViaPigeonAsBot
			} else {
				senderName = "sent to pigeon by " + resolver.UserName(ctx, msg.User)
				senderID = msg.User
				via = modelv1.ViaToPigeon
			}

			if err := ms.Write(ch.ID, channelName, senderName, senderID, text, ts, msg.Timestamp, via); err != nil {
				slog.WarnContext(ctx, "slack sync: bot DM write failed", "error", err)
				continue
			}
			written++
			syncReactions(ctx, ms, resolver, channelName, msg)
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
			isReply := reply.Timestamp != msg.Timestamp
			syncMessage(ctx, ms, resolver, channelName, reply, func(sender, senderID, text string, ts time.Time) error {
				return ms.WriteThreadMessage(channelName, msg.Timestamp, sender, senderID, text, ts, reply.Timestamp, isReply, modelv1.ViaOrganic)
			})
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
					if ctxMsg.BotID != "" || ctxMsg.Text == "" {
						continue
					}
					if ctxMsg.SubType != "" && ctxMsg.SubType != "thread_broadcast" {
						continue
					}
					userName := resolver.UserName(ctx, ctxMsg.User)
					text := resolver.ResolveText(ctx, ctxMsg.Text)
					ts := ParseTimestamp(ctxMsg.Timestamp)
					if err := ms.WriteThreadContext(channelName, msg.Timestamp, userName, ctxMsg.User, text, ts, ctxMsg.Timestamp); err != nil {
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

