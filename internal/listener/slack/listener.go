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
)

// Listener receives Slack Socket Mode events and writes messages to local text files.
// Each listener handles a single workspace with its own Socket Mode connection.
// On every Socket Mode connect (including reconnects), it runs a sync to backfill
// any messages missed while disconnected.
type Listener struct {
	client    *socketmode.Client
	resolver  *Resolver
	messages  *MessageStore
	userToken string
	botToken  string
	acct      account.Account
	teamID    string
	botUserID string // bot's Slack user ID, used to detect @mentions
	onMessage hub.MessageNotifyFunc
}

// NewListener creates a Slack listener for a single workspace.
// botUserID is the bot's Slack user ID (used to detect @mentions).
// onMessage is called (if non-nil) when a routable message arrives:
// DMs, multi-party DMs, private channel posts, or bot mentions.
func NewListener(client *socketmode.Client, resolver *Resolver, messages *MessageStore, userToken, botToken string, acct account.Account, teamID, botUserID string, onMessage hub.MessageNotifyFunc) *Listener {
	return &Listener{
		client:    client,
		resolver:  resolver,
		messages:  messages,
		userToken: userToken,
		botToken:  botToken,
		acct:      acct,
		teamID:    teamID,
		botUserID: botUserID,
		onMessage: onMessage,
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
					if err := Sync(ctx, l.userToken, l.botToken, l.resolver, l.acct, l.messages); err != nil {
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

	// Skip system events (channel_join, channel_topic, etc.) and messages with
	// empty text. Allow bot_message and thread_broadcast subtypes through —
	// bot messages contain valuable info (alerts, CI, integrations).
	if msg.Text == "" || !allowedSubType(msg.SubType) {
		slog.WarnContext(ctx, "slack: skipping message",
			"channel", msg.Channel, "ts", msg.TimeStamp,
			"botID", msg.BotID, "subType", msg.SubType,
			"emptyText", msg.Text == "", "account", l.acct)
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
	channelName := l.resolver.ChannelName(ctx, msg.Channel)
	// For bot DMs, label the sender. ChannelName already resolves the bot's DM
	// channel to the same "@Username" as the user's DM, so messages interleave.
	isBotDM := (msg.ChannelType == "im" || msg.ChannelType == "mpim") && !l.resolver.IsMember(msg.Channel)
	var via modelv1.Via
	if isBotDM {
		userName = "sent to pigeon by " + userName
		via = modelv1.ViaToPigeon
	}
	text := l.resolver.ResolveText(ctx, msg.Text)
	ts := ParseTimestamp(msg.TimeStamp)

	isThreadReply := msg.ThreadTimeStamp != "" && msg.ThreadTimeStamp != msg.TimeStamp

	// Write to channel date file unless it's a thread-only reply.
	// thread_broadcast replies appear in both channel and thread.
	if !isThreadReply || msg.SubType == "thread_broadcast" {
		if err := l.messages.Write(msg.Channel, channelName, userName, userID, text, ts, msg.TimeStamp, via); err != nil {
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

		if err := l.messages.WriteThreadMessage(channelName, msg.ThreadTimeStamp, userName, userID, text, ts, msg.TimeStamp, true, via); err != nil {
			slog.ErrorContext(ctx, "failed to write thread reply", "error", err,
				"account", l.acct, "thread_ts", msg.ThreadTimeStamp)
		}
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "account", l.acct, "text_len", len(msg.Text))

	// Write .meta.json for the conversation.
	var meta modelv1.ConversationMeta
	switch msg.ChannelType {
	case "im":
		meta = modelv1.NewSlackDMMeta(channelName, msg.Channel, l.resolver.DMUserID(channelName))
	case "mpim":
		meta = modelv1.NewSlackGroupDMMeta(channelName, msg.Channel)
	default:
		meta = modelv1.NewSlackChannelMeta(channelName, msg.Channel)
	}
	if err := l.messages.store.WriteMeta(l.acct, channelName, meta); err != nil {
		slog.WarnContext(ctx, "failed to write .meta.json", "channel", channelName, "error", err)
	}

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
	if parent.Text == "" {
		return
	}
	userName, userID, err := l.resolver.SenderName(ctx, parent.User, parent.BotID, parent.Username)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve thread parent sender", "error", err,
			"account", l.acct, "thread_ts", threadTS)
		return
	}
	text := l.resolver.ResolveText(ctx, parent.Text)
	ts := ParseTimestamp(parent.Timestamp)
	if err := l.messages.WriteThreadMessage(channelName, threadTS, userName, userID, text, ts, parent.Timestamp, false, modelv1.ViaOrganic); err != nil {
		slog.WarnContext(ctx, "failed to write thread parent", "error", err,
			"account", l.acct, "thread_ts", threadTS)
	}
}

// handleReaction stores an incoming reaction (or unreaction) event.
func (l *Listener) handleReaction(ctx context.Context, userID, emoji string, item slackevents.Item, remove bool) {
	if item.Type != "message" {
		return
	}

	channelName := l.resolver.ChannelName(ctx, item.Channel)
	userName := l.resolver.UserName(ctx, userID)

	if err := writeReaction(l.messages, channelName, item.Timestamp, userName, userID, emoji, remove); err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack reaction saved",
		"emoji", emoji, "from", userName, "channel", channelName, "remove", remove, "account", l.acct)
}

// handleEdit stores a message edit event.
func (l *Listener) handleEdit(ctx context.Context, msg *slackevents.MessageEvent) {
	if msg.Message == nil {
		return
	}

	channelName := l.resolver.ChannelName(ctx, msg.Channel)
	userName := l.resolver.UserName(ctx, msg.Message.User)
	text := l.resolver.ResolveText(ctx, msg.Message.Text)
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

	if err := l.messages.AppendEdit(channelName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store edit", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack edit saved",
		"msg_id", msg.Message.Timestamp, "channel", channelName, "account", l.acct)
}

// handleDelete stores a message delete event.
func (l *Listener) handleDelete(ctx context.Context, msg *slackevents.MessageEvent) {
	channelName := l.resolver.ChannelName(ctx, msg.Channel)
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
		senderName = l.resolver.UserName(ctx, msg.PreviousMessage.User)
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

	if err := l.messages.AppendDelete(channelName, line); err != nil {
		slog.ErrorContext(ctx, "failed to store delete", "error", err, "account", l.acct)
	}

	slog.InfoContext(ctx, "slack delete saved",
		"msg_id", deletedTS, "channel", channelName, "account", l.acct)
}
