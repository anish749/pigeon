package slack

import (
	"context"
	"log/slog"
	"sync"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish/claude-msg-utils/internal/store"
)

// workspaceHandler holds the per-workspace state needed to process events.
type workspaceHandler struct {
	resolver  *Resolver
	workspace string
}

// Listener receives Slack Socket Mode events and routes them to the correct
// workspace handler based on team ID. A single Socket Mode connection serves
// all workspaces since they share the same app token.
type Listener struct {
	client   *socketmode.Client
	mu       sync.RWMutex
	handlers map[string]*workspaceHandler // team ID → handler
}

// New creates a Slack listener backed by a single Socket Mode connection.
func New(client *socketmode.Client) *Listener {
	return &Listener{
		client:   client,
		handlers: make(map[string]*workspaceHandler),
	}
}

// AddWorkspace registers a workspace so its events are handled.
// Safe to call while Run is active (e.g. after an OAuth install).
func (l *Listener) AddWorkspace(teamID, workspace string, resolver *Resolver) {
	l.mu.Lock()
	l.handlers[teamID] = &workspaceHandler{resolver: resolver, workspace: workspace}
	l.mu.Unlock()
	slog.Info("slack listener: workspace registered", "workspace", workspace, "team_id", teamID)
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
				slog.InfoContext(ctx, "slack: connected via Socket Mode")
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				l.client.Ack(*evt.Request)
				l.handleEvent(ctx, eventsAPIEvent)
			case socketmode.EventTypeErrorBadMessage:
				slog.WarnContext(ctx, "slack: bad message")
			case socketmode.EventTypeIncomingError:
				slog.ErrorContext(ctx, "slack: incoming error")
			}
		}
	}
}

func (l *Listener) handleEvent(ctx context.Context, evt slackevents.EventsAPIEvent) {
	l.mu.RLock()
	handler, ok := l.handlers[evt.TeamID]
	l.mu.RUnlock()
	if !ok {
		slog.WarnContext(ctx, "slack: event from unknown team, ignoring", "team_id", evt.TeamID)
		return
	}

	switch innerEvt := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		handler.handleMessage(ctx, innerEvt)
	}
}

func (h *workspaceHandler) handleMessage(ctx context.Context, msg *slackevents.MessageEvent) {
	// Skip bot messages and subtypes (edits, deletes, joins, etc.)
	if msg.BotID != "" || msg.SubType != "" || msg.Text == "" {
		return
	}

	userName := h.resolver.UserName(ctx, msg.User)
	channelName := h.resolver.ChannelName(ctx, msg.Channel)
	text := h.resolver.ResolveText(ctx, msg.Text)
	ts := ParseTimestamp(msg.TimeStamp)

	if err := store.WriteMessage("slack", h.workspace, channelName, userName, text, ts); err != nil {
		slog.ErrorContext(ctx, "failed to write slack message", "error", err, "workspace", h.workspace)
		return
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "workspace", h.workspace, "text_len", len(msg.Text))
}
