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
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Listener receives Slack Socket Mode events and writes messages to local text files.
// Each listener handles a single workspace with its own Socket Mode connection.
// On every Socket Mode connect (including reconnects), it runs a sync to backfill
// any messages missed while disconnected.
type Listener struct {
	client       *socketmode.Client
	resolver     *Resolver
	messages     *MessageStore
	userToken    string
	botToken     string
	acct         account.Account
	teamID       string
	pigeonBotUID string // Slack user ID of the Pigeon bot, used to detect @mentions and self-messages
	appName      string
	onEvent      hub.NotifyFunc
	syncTracker  *syncstatus.Tracker
}

// NewListener creates a Slack listener for a single workspace.
// pigeonBotUID is the Slack user ID of the Pigeon bot (used to detect
// @mentions and self-messages). appName is the configured app display
// name shown in the no-session auto-reply text. onEvent is the single
// hub callback used for every routable platform event — messages,
// reactions, edits, deletes — built via hub.NewMsg / NewReact /
// NewEdit / NewDelete at the call sites. Must be non-nil.
func NewListener(client *socketmode.Client, resolver *Resolver, messages *MessageStore, userToken, botToken string, acct account.Account, teamID, pigeonBotUID, appName string, onEvent hub.NotifyFunc, syncTracker *syncstatus.Tracker) *Listener {
	return &Listener{
		client:       client,
		resolver:     resolver,
		messages:     messages,
		userToken:    userToken,
		botToken:     botToken,
		acct:         acct,
		teamID:       teamID,
		pigeonBotUID: pigeonBotUID,
		appName:      appName,
		onEvent:      onEvent,
		syncTracker:  syncTracker,
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
		if l.pigeonBotUID != "" && innerEvt.User == l.pigeonBotUID {
			l.resolver.AddBotMember(innerEvt.Channel)
		}
		slog.InfoContext(ctx, "slack: member joined channel",
			"channel", innerEvt.Channel, "user", innerEvt.User, "account", l.acct)
	case *slackevents.MemberLeftChannelEvent:
		l.resolver.RemoveMember(innerEvt.Channel)
		if l.pigeonBotUID != "" && innerEvt.User == l.pigeonBotUID {
			l.resolver.RemoveBotMember(innerEvt.Channel)
		}
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

	if msg.Message == nil {
		return
	}
	if !shouldKeepMessage(*msg.Message) {
		logDroppedMessage(ctx, *msg.Message, msg.Channel, "slack listener")
		return
	}

	// Skip messages from channels the user hasn't joined.
	// Always allow DMs and group DMs through — the bot only receives these for
	// its own conversations, and the bot owner should see all messages to the bot.
	if msg.ChannelType != "im" && msg.ChannelType != "mpim" && !l.resolver.IsMember(msg.Channel) {
		return
	}

	rs, err := l.resolver.ResolveSender(ctx, msg.Channel, *msg.Message)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping message, cannot resolve sender",
			"channel", msg.Channel, "ts", msg.TimeStamp, "error", err, "account", l.acct)
		return
	}
	l.messages.EnsureMeta(rs.ChannelName, l.resolver.ConvMeta(msg.Channel, rs.ChannelName))
	isBotDM := (msg.ChannelType == "im" || msg.ChannelType == "mpim") && !l.resolver.IsMember(msg.Channel)
	via := DetermineVia(*msg.Message, isBotDM)
	text, err := l.resolver.ResolveText(ctx, msg.Text)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping message, cannot resolve text",
			"channel", rs.ChannelName, "ts", msg.TimeStamp, "error", err, "account", l.acct)
		return
	}
	ts := ParseTimestamp(msg.TimeStamp)

	isThreadReply := msg.ThreadTimeStamp != "" && msg.ThreadTimeStamp != msg.TimeStamp

	// Write to the channel date file and/or thread file. The MsgLine
	// returned by whichever write fires is what we route to the hub.
	// thread_broadcast replies appear in both files; channel write wins
	// as the routing payload in that case.
	raw := slackraw.NewSlackRawContent(*msg.Message)

	var payload modelv1.MsgLine
	var have bool
	if !isThreadReply || msg.SubType == "thread_broadcast" {
		p, err := l.messages.Write(rs, text, ts, msg.TimeStamp, via, raw)
		if err != nil {
			slog.ErrorContext(ctx, "failed to write slack message", "error", err, "account", l.acct)
			return
		}
		payload, have = p, true
	}

	if isThreadReply {
		// Only fetch the parent from Slack API if the thread file doesn't exist yet.
		if !l.messages.ThreadExists(rs.ChannelName, msg.ThreadTimeStamp) {
			l.ensureThreadParent(ctx, msg.Channel, msg.ThreadTimeStamp)
		}

		p, err := l.messages.WriteThreadMessage(rs, msg.ThreadTimeStamp, text, ts, msg.TimeStamp, true, via, raw)
		if err != nil {
			slog.ErrorContext(ctx, "failed to write thread reply", "error", err,
				"account", l.acct, "thread_ts", msg.ThreadTimeStamp)
		}
		if !have {
			payload = p
		}
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", rs.SenderName, "channel", rs.ChannelName, "account", l.acct, "text_len", len(msg.Text))

	// Notify the hub for messages the user cares about:
	//   - DMs (im) and multi-party DMs (mpim) — always
	//   - Private channels (group) — always (user opted in by joining)
	//   - Public channels — when the bot is @mentioned, OR the message is a
	//     reply in a thread the bot already participates in (mentioned or
	//     posted there earlier)
	var result hub.RouteResult
	switch msg.ChannelType {
	case "im", "mpim":
		result = l.onEvent(hub.NewMsg(l.acct, rs.ChannelName, payload))
	case "group":
		result = l.onEvent(hub.NewMsg(l.acct, rs.ChannelName, payload))
	case "channel":
		if l.shouldRouteChannelMessage(msg, isThreadReply, rs.ChannelName) {
			result = l.onEvent(hub.NewMsg(l.acct, rs.ChannelName, payload))
		}
	default:
		slog.WarnContext(ctx, "unrecognized channel type, message not routed to hub",
			"channel_type", msg.ChannelType, "channel", rs.ChannelName, "account", l.acct)
	}

	// Auto-reply when someone DMs the bot but no pigeon session is configured.
	if shouldAutoReply(l.pigeonBotUID, msg, result.State, isBotDM) {
		botAPI := goslack.New(l.botToken)
		_, _, err := botAPI.PostMessageContext(ctx, msg.Channel,
			goslack.MsgOptionText("The user you're trying to reach hasn't finished setting up "+l.appName+", so this message won't be delivered. Please reach out to them directly and ask them to complete their "+l.appName+" setup.", false),
			goslack.MsgOptionMetadata(PigeonSendMetadata(modelv1.ViaPigeonAsBot)))
		if err != nil {
			slog.ErrorContext(ctx, "failed to send auto-reply", "error", err, "account", l.acct)
		}
	}
}

// shouldRouteChannelMessage decides whether a public-channel message should
// be forwarded to the hub. The bot is forwarded:
//   - any message whose raw event text @-mentions the bot, or
//   - any thread reply in a thread the bot already participates in
//     (mentioned earlier or has posted there).
//
// The mention check uses msg.Text directly because the Slack event payload
// carries the unresolved <@UID> markup; the resolved-text form on disk is
// used by BotParticipatesInThread for the historical view.
func (l *Listener) shouldRouteChannelMessage(msg *slackevents.MessageEvent, isThreadReply bool, channelName string) bool {
	if l.pigeonBotUID == "" {
		return false
	}
	if strings.Contains(msg.Text, "<@"+l.pigeonBotUID+">") {
		return true
	}
	if isThreadReply && l.messages.BotParticipatesInThread(channelName, msg.ThreadTimeStamp, l.pigeonBotUID) {
		return true
	}
	return false
}

// ensureThreadParent fetches the parent message of a thread and writes it to the
// thread file. Called when a real-time thread reply arrives but no thread file exists.
func (l *Listener) ensureThreadParent(ctx context.Context, channelID, threadTS string) {
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
	parentRS, err := l.resolver.ResolveSender(ctx, channelID, parent.Msg)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve thread parent sender", "error", err,
			"account", l.acct, "thread_ts", threadTS)
		return
	}
	text, err := l.resolver.ResolveText(ctx, parent.Text)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve thread parent text", "error", err,
			"account", l.acct, "thread_ts", threadTS)
		return
	}
	ts := ParseTimestamp(parent.Timestamp)
	if _, err := l.messages.WriteThreadMessage(parentRS, threadTS, text, ts, parent.Timestamp, false, modelv1.ViaOrganic, slackraw.NewSlackRawContent(parent.Msg)); err != nil {
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

	// Route to the thread file when the target message is a thread reply.
	threadTS := ""
	if m := l.messages.LookupMessage(channelName, item.Timestamp); m != nil {
		threadTS = m.ThreadTS
	}

	react, err := l.messages.AppendReaction(channelName, item.Timestamp, threadTS, userName, userID, emoji, remove)
	if err != nil {
		slog.ErrorContext(ctx, "failed to store reaction", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "slack reaction saved",
		"emoji", emoji, "from", userName, "channel", channelName, "remove", remove, "account", l.acct)

	// Route the reaction to the connected session. The listener only sees
	// reactions for channels the bot has visibility into (DMs/MPDMs and
	// channels it's a member of), so there is no additional filter here.
	res := l.onEvent(hub.NewReact(l.acct, channelName, react))
	slog.InfoContext(ctx, "slack reaction routed", "result", res, "account", l.acct)
}

// handleEdit stores a message edit event and routes it to the connected
// session.
func (l *Listener) handleEdit(ctx context.Context, msg *slackevents.MessageEvent) {
	if msg.Message == nil {
		return
	}

	rs, err := l.resolver.ResolveSender(ctx, msg.Channel, *msg.Message)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping edit, cannot resolve",
			"channel", msg.Channel, "user_id", msg.Message.User,
			"bot_id", msg.Message.BotID, "username", msg.Message.Username,
			"error", err, "account", l.acct)
		return
	}
	text, err := l.resolver.ResolveText(ctx, msg.Message.Text)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping edit, cannot resolve text",
			"channel", rs.ChannelName, "error", err, "account", l.acct)
		return
	}
	ts := time.Now().UTC()
	threadTS := msg.Message.ThreadTimestamp
	raw := slackraw.NewSlackRawContent(*msg.Message)

	edit, err := l.messages.AppendEdit(rs, msg.Message.Timestamp, threadTS, text, ts, raw)
	if err != nil {
		slog.ErrorContext(ctx, "failed to store edit", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "slack edit saved",
		"msg_id", msg.Message.Timestamp, "channel", rs.ChannelName, "account", l.acct)

	res := l.onEvent(hub.NewEdit(l.acct, rs.ChannelName, edit))
	slog.InfoContext(ctx, "slack edit routed", "result", res, "account", l.acct)
}

// handleDelete stores a message delete event and routes it to the
// connected session.
func (l *Listener) handleDelete(ctx context.Context, msg *slackevents.MessageEvent) {
	channelName, err := l.resolver.ChannelName(ctx, msg.Channel)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping delete, cannot resolve channel",
			"channel", msg.Channel, "error", err, "account", l.acct)
		return
	}
	ts := time.Now().UTC()

	// For message_deleted, the deleted message's timestamp is in msg.PreviousMessage
	// or msg.DeletedTimeStamp. PreviousMessage is also the only source for the
	// deleted message's sender and thread context — without it we'd emit a
	// notification with empty sender fields, which is worse than dropping the
	// event entirely.
	if msg.PreviousMessage == nil {
		slog.WarnContext(ctx, "slack delete: no previous message snapshot, skipping",
			"channel", channelName, "deleted_ts", msg.DeletedTimeStamp, "account", l.acct)
		return
	}
	deletedTS := msg.DeletedTimeStamp
	if deletedTS == "" {
		deletedTS = msg.PreviousMessage.Timestamp
	}
	if deletedTS == "" {
		slog.WarnContext(ctx, "slack delete: no deleted timestamp", "channel", channelName, "account", l.acct)
		return
	}

	name, id, err := l.resolver.SenderName(ctx, *msg.PreviousMessage)
	if err != nil {
		slog.WarnContext(ctx, "slack: skipping delete, cannot resolve sender",
			"user_id", msg.PreviousMessage.User, "bot_id", msg.PreviousMessage.BotID, "username", msg.PreviousMessage.Username,
			"channel", channelName, "error", err, "account", l.acct)
		return
	}
	rs := ResolvedSender{
		ChannelName: channelName,
		SenderName:  name,
		SenderID:    id,
	}

	threadTS := msg.PreviousMessage.ThreadTimestamp
	del, err := l.messages.AppendDelete(rs, deletedTS, threadTS, ts)
	if err != nil {
		slog.ErrorContext(ctx, "failed to store delete", "error", err, "account", l.acct)
		return
	}

	slog.InfoContext(ctx, "slack delete saved",
		"msg_id", deletedTS, "channel", rs.ChannelName, "account", l.acct)

	res := l.onEvent(hub.NewDelete(l.acct, rs.ChannelName, del))
	slog.InfoContext(ctx, "slack delete routed", "result", res, "account", l.acct)
}
