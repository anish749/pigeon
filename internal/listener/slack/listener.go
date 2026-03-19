package slack

import (
	"context"
	"log/slog"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Listener receives Slack Socket Mode events and writes messages to local text files.
// Each listener handles a single workspace with its own Socket Mode connection.
type Listener struct {
	client    *socketmode.Client
	resolver  *Resolver
	messages  *MessageStore
	workspace string
	teamID    string
}

// New creates a Slack listener for a single workspace.
func New(client *socketmode.Client, resolver *Resolver, messages *MessageStore, workspace, teamID string) *Listener {
	return &Listener{client: client, resolver: resolver, messages: messages, workspace: workspace, teamID: teamID}
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
				slog.ErrorContext(ctx, "slack: incoming error", "workspace", l.workspace, "error", evt.Data)
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
	}
}

func (l *Listener) handleMessage(ctx context.Context, msg *slackevents.MessageEvent) {
	if msg.BotID != "" || msg.SubType != "" || msg.Text == "" {
		return
	}

	userName := l.resolver.UserName(ctx, msg.User)
	channelName := l.resolver.ChannelName(ctx, msg.Channel)
	text := l.resolver.ResolveText(ctx, msg.Text)
	ts := ParseTimestamp(msg.TimeStamp)

	if err := l.messages.Write(msg.Channel, channelName, userName, text, ts, msg.TimeStamp); err != nil {
		slog.ErrorContext(ctx, "failed to write slack message", "error", err, "workspace", l.workspace)
		return
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "workspace", l.workspace, "text_len", len(msg.Text))
}
