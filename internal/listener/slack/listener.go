package slack

import (
	"context"
	"log/slog"
	"strings"
	"time"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Listener receives Slack Socket Mode events and writes messages to local text files.
// Each listener handles a single workspace with its own Socket Mode connection.
// On every Socket Mode connect (including reconnects), it runs a sync to backfill
// any messages missed while disconnected.
type Listener struct {
	client      *socketmode.Client
	resolver    *Resolver
	messages    *MessageStore
	userToken   string
	botToken    string
	acct        account.Account
	teamID      string
	botUserID   string // bot's Slack user ID, used to detect @mentions
	onMessage   hub.MessageNotifyFunc
	onReaction  hub.ReactionNotifyFunc
	syncTracker *syncstatus.Tracker
}

// NewListener creates a Slack listener for a single workspace.
// botUserID is the bot's Slack user ID (used to detect @mentions).
// onMessage is called (if non-nil) when a routable message arrives:
// DMs, multi-party DMs, private channel posts, or bot mentions.
// onReaction is called (if non-nil) when a reaction or unreaction event arrives.
func NewListener(client *socketmode.Client, resolver *Resolver, messages *MessageStore, userToken, botToken string, acct account.Account, teamID, botUserID string, onMessage hub.MessageNotifyFunc, onReaction hub.ReactionNotifyFunc, syncTracker *syncstatus.Tracker) *Listener {
	return &Listener{
		client:      client,
		resolver:    resolver,
		messages:    messages,
		userToken:   userToken,
		botToken:    botToken,
		acct:        acct,
		teamID:      teamID,
		botUserID:   botUserID,
		onMessage:   onMessage,
		onReaction:  onReaction,
		syncTracker: syncTracker,
	}
}

// Run starts the event loop. It blocks until ctx is cancelled.
func (l *Listener) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-l.client.Events:
			if !ok {
				return
			}
			switch evt.Type {
			case socketmode.EventTypeConnected:
				slog.InfoContext(ctx, "slack: connected, triggering sync", "account", l.acct)
				go func() {
					if err := Sync(ctx, l.userToken, l.botToken, l.resolver, l.acct, l.messages, l.syncTracker); err != nil {
						slog.ErrorContext(ctx, "slack sync failed", "account", l.acct, "error", err)
					}
				}()
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				l.client.Ack(*evt.Request)
				l.handleEvent(ctx, eventsAPIEvent)
			case socketmode.EventTypeErrorBadMessage:
				slog.WarnContext(ctx, "slack: bad message", "account", l.acct)
			case socketmode.EventTypeIncomingError:
				slog.ErrorContext(ctx, "slack: incoming error", "account", l.acct, "error", evt.Data)
			}
		}
	}
}

func (l *Listener) handleEvent(ctx context.Context, evt slackevents.EventsAPIEvent) {
	// Safety check: ignore events from other teams (shouldn't happen with per-app connections)
	if l.teamID != "" && evt.TeamID != l.teamID {
		return
	}
	switch innerEvt := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		l.handleMessage(ctx, innerEvt)
	case *slackevents.ReactionAddedEvent:
		l.handleReaction(ctx, innerEvt.User, innerEvt.Reaction, innerEvt.Item, false)
	case *slackevents.ReactionRemovedEvent:
		l.handleReaction(ctx, innerEvt.User, innerEvt.Reaction, innerEvt.Item, true)
	case *slackevents.MemberJoinedChannelEvent:
		l.resolver.AddMember(innerEvt.Channel)
		slog.InfoContext(ctx, "slack: member joined channel",
			"channel", innerEvt.Channel, "user", innerEvt.User, "account", l.acct)
	case *slackevents.MemberLeftChannelEvent:
		l.resolver.RemoveMember(innerEvt.Channel)
		slog.InfoContext(ctx, "slack: member left channel",
			"channel", innerEvt.Channel, "user", innerEvt.User, "account", l.acct)
	}
}

func (l *Listener) handleMessage(ctx context.Context, msg *slackevents.MessageEvent) {
	// Handle edits and deletes before the general filter.
	switch msg.SubType {
	case "message_changed":
		l.handleEdit(ctx, msg)
		return
	case "message_deleted":
		l.handleDelete(ctx, msg)
		return
	}

	// Skip system events (channel_join, channel_topic, etc.).
	// Allow bot_message and thread_broadcast subtypes through —
	// bot messages contain valuable info (alerts, CI, integrations).
	if !allowedSubType(msg.SubType) {
		return
	}

	// Determine whether this message has text or only block content.
	// Messages with empty Text but non-empty blocks/attachments are stored
	// as SlackBlock lines (raw JSON), similar to how GWS and Linear store
	// structured data.
	hasBlocks := msg.Message != nil && (len(msg.Message.Blocks.BlockSet) > 0 || len(msg.Message.Attachments) > 0)
	if msg.Text == "" && !hasBlocks {
		return
	}

	// Skip messages from channels the user hasn't joined.
	// Always allow DMs and group DMs through — the bot only receives these for
	// its own conversations, and the bot owner should see all messages to the bot.
	if msg.ChannelType != "im" && msg.ChannelType != "mpim" && !l.resolver.IsMember(msg.Channel) {
		return
	}

	userName, userID, err := l.resolver.SenderName(ctx, msg.User, msg.BotID, msg.Username)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping message, cannot resolve sender",
			"channel", msg.Channel, "ts", msg.TimeStamp, "error", err, "account", l.acct)
		return
	}
	channelName, err := l.resolver.ChannelName(ctx, msg.Channel)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping message, cannot resolve channel",
			"channel", msg.Channel, "ts", msg.TimeStamp, "error", err, "account", l.acct)
		return
	}
	l.messages.EnsureMeta(channelName, l.resolver.ConvMeta(msg.Channel, channelName))
	// For bot DMs, label the sender. ChannelName already resolves the bot's DM
	// channel to the same "@Username" as the user's DM, so messages interleave.
	isBotDM := (msg.ChannelType == "im" || msg.ChannelType == "mpim") && !l.resolver.IsMember(msg.Channel)
	var via modelv1.Via
	if isBotDM {
		userName = "sent to pigeon by " + userName
		via = modelv1.ViaToPigeon
	}
	ts := ParseTimestamp(msg.TimeStamp)
	isThreadReply := msg.ThreadTimeStamp != "" && msg.ThreadTimeStamp != msg.TimeStamp

	// Build the line to write. Text messages use MsgLine; block-only
	// messages use SlackBlock to preserve the raw structured data.
	var line modelv1.Line
	if msg.Text != "" {
		text, err := l.resolver.ResolveText(ctx, msg.Text)
		if err != nil {
			slog.WarnContext(ctx, "slack: skipping message, cannot resolve text",
				"channel", channelName, "ts", msg.TimeStamp, "error", err, "account", l.acct)
			return
		}
		line = modelv1.NewMsgLine(msg.TimeStamp, ts, userName, userID, text, via, false)
	} else {
		var err error
		line, err = modelv1.NewSlackBlockLine(slackBlockPayload(msg.TimeStamp, ts, userName, userID, via, isThreadReply, msg.Message.Blocks, msg.Message.Attachments))
		if err != nil {
			slog.ErrorContext(ctx, "failed to build slack block line", "error", err, "account", l.acct)
			return
		}
	}

	// Write to channel date file unless it's a thread-only reply.
	// thread_broadcast replies appear in both channel and thread.
	if !isThreadReply || msg.SubType == "thread_broadcast" {
		if err := l.messages.Append(channelName, line); err != nil {
			slog.ErrorContext(ctx, "failed to write slack message", "error", err, "account", l.acct)
			return
		}
	}

	// Write thread replies to the thread file
	if isThreadReply {
		// Only fetch the parent from Slack API if the thread file doesn't exist yet.
		if !l.messages.ThreadExists(channelName, msg.ThreadTimeStamp) {
			l.ensureThreadParent(ctx, msg.Channel, channelName, msg.ThreadTimeStamp)
		}

		if line.Type == modelv1.LineMessage {
			line.Msg.Reply = true
		}
		if err := l.messages.AppendThread(channelName, msg.ThreadTimeStamp, line); err != nil {
			slog.ErrorContext(ctx, "failed to write thread reply", "error", err,
				"account", l.acct, "thread_ts", msg.ThreadTimeStamp)
		}
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "account", l.acct, "text_len", len(msg.Text))

	// Notify the hub for messages the user cares about:
	//   - DMs (im) and multi-party DMs (mpim) — always
	//   - Private channels (group) — always (user opted in by joining)
	//   - Public channels — only when the bot is @mentioned
	var result hub.RouteResult
	switch msg.ChannelType {
	case "im", "mpim":
		result = l.onMessage(l.acct, channelName)
	case "group":
		result = l.onMessage(l.acct, channelName)
	case "channel":
		if l.botUserID != "" && strings.Contains(msg.Text, "<@"+l.botUserID+">") {
			result = l.onMessage(l.acct, channelName)
		}
	default:
		slog.WarnContext(ctx, "unrecognized channel type, message not routed to hub",
			"channel_type", msg.ChannelType, "channel", channelName, "account", l.acct)
	}

	// Auto-reply when someone DMs the bot but no pigeon session is configured.
	if isBotDM && result.State == hub.RouteNoSession {
		botAPI := goslack.New(l.botToken)
		_, _, err := botAPI.PostMessageContext(ctx, msg.Channel,
			goslack.MsgOptionText("The user you're trying to reach hasn't finished setting up Pigeon, so this message won't be delivered. Please reach out to them directly and ask them to complete their Pigeon setup.", false))
		if err != nil {
			slog.ErrorContext(ctx, "failed to send auto-reply", "error", err, "account", l.acct)
		}
	}
}

// ensureThreadParent fetches the parent message of a thread and writes it to the
// thread file. Called when a real-time thread reply arrives but no thread file exists.
func (l *Listener) ensureThreadParent(ctx context.Context, channelID, channelName, threadTS string) {
	api := goslack.New(l.userToken)
	msgs, _, _, err := api.GetConversationRepliesContext(ctx, &goslack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     1,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch thread parent", "error", err,
			"account", l.acct, "thread_ts", threadTS)
		return
	}
	if len(msgs) == 0 {
		return
	}
	parent := msgs[0]
	hasBlocks := len(parent.Blocks.BlockSet) > 0 || len(parent.Attachments) > 0
	if parent.Text == "" && !hasBlocks {
		return
	}
	userName, userID, err := l.resolver.SenderName(ctx, parent.User, parent.BotID, parent.Username)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve thread parent sender", "error", err,
			"account", l.acct, "thread_ts", threadTS)
		return
	}
	ts := ParseTimestamp(parent.Timestamp)
	var line modelv1.Line
	if parent.Text != "" {
		text, err := l.resolver.ResolveText(ctx, parent.Text)
		if err != nil {
			slog.WarnContext(ctx, "failed to resolve thread parent text", "error", err,
				"account", l.acct, "thread_ts", threadTS)
			return
		}
		line = modelv1.NewMsgLine(parent.Timestamp, ts, userName, userID, text, modelv1.ViaOrganic, false)
	} else {
		var err error
		line, err = modelv1.NewSlackBlockLine(slackBlockPayload(parent.Timestamp, ts, userName, userID, modelv1.ViaOrganic, false, parent.Blocks, parent.Attachments))
		if err != nil {
			slog.WarnContext(ctx, "failed to build thread parent block line", "error", err, "account", l.acct)
			return
		}
	}
	if err := l.messages.AppendThread(channelName, threadTS, line); err != nil {
		slog.WarnContext(ctx, "failed to write thread parent", "error", err,
			"account", l.acct, "thread_ts", threadTS)
	}
}

// handleReaction stores an incoming reaction (or unreaction) event.
func (l *Listener) handleReaction(ctx context.Context, userID, emoji string, item slackevents.Item, remove bool) {
	if item.Type != "message" {
		return
	}

	channelName, err := l.resolver.ChannelName(ctx, item.Channel)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping reaction, cannot resolve channel",
			"channel", item.Channel, "error", err, "account", l.acct)
		return
	}
	userName, err := l.resolver.UserName(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping reaction, cannot resolve user",
			"user_id", userID, "channel", channelName, "error", err, "account", l.acct)
		return
	}

	if err := writeReaction(l.messages, channelName, item.Timestamp, userName, userID, emoji, remove); err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack reaction saved",
		"emoji", emoji, "from", userName, "channel", channelName, "remove", remove, "account", l.acct)

	// Route the reaction to the connected session. The listener only sees
	// reactions for channels the bot has visibility into (DMs/MPDMs and
	// channels it's a member of), so there is no additional filter here.
	if l.onReaction == nil {
		return
	}
	l.onReaction(l.acct, channelName, hub.ReactionInfo{
		MsgID:    item.Timestamp,
		Sender:   userName,
		SenderID: userID,
		Emoji:    emoji,
		Remove:   remove,
	})
}

// handleEdit stores a message edit event.
func (l *Listener) handleEdit(ctx context.Context, msg *slackevents.MessageEvent) {
	if msg.Message == nil {
		return
	}

	channelName, err := l.resolver.ChannelName(ctx, msg.Channel)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping edit, cannot resolve channel",
			"channel", msg.Channel, "error", err, "account", l.acct)
		return
	}
	userName, err := l.resolver.UserName(ctx, msg.Message.User)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping edit, cannot resolve user",
			"user_id", msg.Message.User, "channel", channelName, "error", err, "account", l.acct)
		return
	}
	text, err := l.resolver.ResolveText(ctx, msg.Message.Text)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping edit, cannot resolve text",
			"channel", channelName, "error", err, "account", l.acct)
		return
	}
	ts := time.Now().UTC()

	line := modelv1.Line{
		Type: modelv1.LineEdit,
		Edit: &modelv1.EditLine{
			Ts:       ts,
			MsgID:    msg.Message.Timestamp,
			Sender:   userName,
			SenderID: msg.Message.User,
			Text:     text,
		},
	}

	if err := l.messages.Append(channelName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store edit", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack edit saved",
		"msg_id", msg.Message.Timestamp, "channel", channelName, "account", l.acct)
}

// handleDelete stores a message delete event.
func (l *Listener) handleDelete(ctx context.Context, msg *slackevents.MessageEvent) {
	channelName, err := l.resolver.ChannelName(ctx, msg.Channel)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping delete, cannot resolve channel",
			"channel", msg.Channel, "error", err, "account", l.acct)
		return
	}
	ts := time.Now().UTC()

	// For message_deleted, the deleted message's timestamp is in msg.PreviousMessage
	// or msg.DeletedTimeStamp.
	deletedTS := msg.DeletedTimeStamp
	if deletedTS == "" && msg.PreviousMessage != nil {
		deletedTS = msg.PreviousMessage.Timestamp
	}
	if deletedTS == "" {
		slog.WarnContext(ctx, "slack delete: no deleted timestamp", "channel", channelName, "account", l.acct)
		return
	}

	var senderName, senderID string
	if msg.PreviousMessage != nil {
		name, err := l.resolver.UserName(ctx, msg.PreviousMessage.User)
		if err != nil {
			slog.WarnContext(ctx, "slack: skipping delete, cannot resolve user",
				"user_id", msg.PreviousMessage.User, "channel", channelName, "error", err, "account", l.acct)
			return
		}
		senderName = name
		senderID = msg.PreviousMessage.User
	}

	line := modelv1.Line{
		Type: modelv1.LineDelete,
		Delete: &modelv1.DeleteLine{
			Ts:       ts,
			MsgID:    deletedTS,
			Sender:   senderName,
			SenderID: senderID,
		},
	}

	if err := l.messages.Append(channelName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store delete", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack delete saved",
		"msg_id", deletedTS, "channel", channelName, "account", l.acct)
}
