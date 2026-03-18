package slack

import (
	"context"
	"log/slog"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish/claude-msg-utils/internal/store"
)

// Listener receives Slack Socket Mode events and writes messages to local text files.
type Listener struct {
	client    *socketmode.Client
	resolver  *Resolver
	workspace string
}

// New creates a Slack listener for the given socket mode client and workspace.
func New(client *socketmode.Client, resolver *Resolver, workspace string) *Listener {
	return &Listener{client: client, resolver: resolver, workspace: workspace}
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
				slog.InfoContext(ctx, "slack: connected via Socket Mode", "workspace", l.workspace)
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				l.client.Ack(*evt.Request)
				l.handleEvent(ctx, eventsAPIEvent)
			case socketmode.EventTypeErrorBadMessage:
				slog.WarnContext(ctx, "slack: bad message", "workspace", l.workspace)
			case socketmode.EventTypeIncomingError:
				slog.ErrorContext(ctx, "slack: incoming error", "workspace", l.workspace)
			}
		}
	}
}

func (l *Listener) handleEvent(ctx context.Context, evt slackevents.EventsAPIEvent) {
	switch innerEvt := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		l.handleMessage(ctx, innerEvt)
	}
}

func (l *Listener) handleMessage(ctx context.Context, msg *slackevents.MessageEvent) {
	// Skip bot messages and subtypes (edits, deletes, joins, etc.)
	if msg.BotID != "" || msg.SubType != "" || msg.Text == "" {
		return
	}

	userName := l.resolver.UserName(ctx, msg.User)
	channelName := l.resolver.ChannelName(ctx, msg.Channel)
	ts := ParseTimestamp(msg.TimeStamp)

	if err := store.WriteMessage("slack", l.workspace, channelName, userName, msg.Text, ts); err != nil {
		slog.ErrorContext(ctx, "failed to write slack message", "error", err, "workspace", l.workspace)
		return
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "workspace", l.workspace, "text_len", len(msg.Text))
}
