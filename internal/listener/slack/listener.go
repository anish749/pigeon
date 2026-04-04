package slack

import (
	"context"
	"log/slog"
	"os"
	"strings"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish/claude-msg-utils/internal/account"
	"github.com/anish/claude-msg-utils/internal/hub"
	"github.com/anish/claude-msg-utils/internal/store"
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
	if msg.BotID != "" || (msg.SubType != "" && msg.SubType != "thread_broadcast") || msg.Text == "" {
		return
	}

	// Skip messages from channels the user hasn't joined.
	// Always allow DMs and group DMs through — the bot only receives these for
	// its own conversations, and the bot owner should see all messages to the bot.
	if msg.ChannelType != "im" && msg.ChannelType != "mpim" && !l.resolver.IsMember(msg.Channel) {
		return
	}

	userName := l.resolver.UserName(ctx, msg.User)
	channelName := l.resolver.ChannelName(ctx, msg.Channel)
	// For bot DMs, label the sender. ChannelName already resolves the bot's DM
	// channel to the same "@Username" as the user's DM, so messages interleave.
	isBotDM := (msg.ChannelType == "im" || msg.ChannelType == "mpim") && !l.resolver.IsMember(msg.Channel)
	if isBotDM {
		userName = "sent to pigeon by " + userName
	}
	text := l.resolver.ResolveText(ctx, msg.Text)
	ts := ParseTimestamp(msg.TimeStamp)

	isThreadReply := msg.ThreadTimeStamp != "" && msg.ThreadTimeStamp != msg.TimeStamp

	// Write to channel date file unless it's a thread-only reply.
	// thread_broadcast replies appear in both channel and thread.
	if !isThreadReply || msg.SubType == "thread_broadcast" {
		if err := l.messages.Write(msg.Channel, channelName, userName, text, ts, msg.TimeStamp); err != nil {
			slog.ErrorContext(ctx, "failed to write slack message", "error", err, "account", l.acct)
			return
		}
	}

	// Write thread replies to the thread file
	if isThreadReply {
		// If thread file doesn't exist yet, fetch and write the parent message first
		threadPath := store.ThreadFilePath(l.acct.Platform, l.acct.NameSlug(), channelName, msg.ThreadTimeStamp)
		if _, err := os.Stat(threadPath); os.IsNotExist(err) {
			l.ensureThreadParent(ctx, msg.Channel, channelName, msg.ThreadTimeStamp)
		}

		if err := l.messages.WriteThreadMessage(channelName, msg.ThreadTimeStamp, userName, text, ts, true); err != nil {
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
	switch msg.ChannelType {
	case "im", "mpim":
		l.onMessage(l.acct, channelName)
	case "group":
		l.onMessage(l.acct, channelName)
	case "channel":
		if l.botUserID != "" && strings.Contains(msg.Text, "<@"+l.botUserID+">") {
			l.onMessage(l.acct, channelName)
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
	userName := l.resolver.UserName(ctx, parent.User)
	text := l.resolver.ResolveText(ctx, parent.Text)
	ts := ParseTimestamp(parent.Timestamp)
	if err := l.messages.WriteThreadMessage(channelName, threadTS, userName, text, ts, false); err != nil {
		slog.WarnContext(ctx, "failed to write thread parent", "error", err,
			"account", l.acct, "thread_ts", threadTS)
	}
}
