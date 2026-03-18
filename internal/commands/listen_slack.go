package commands

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/anish/claude-msg-utils/internal/store"
)

func RunListenSlack(args []string) error {
	fs := flag.NewFlagSet("listen-slack", flag.ExitOnError)
	appToken := fs.String("token", "", "Slack app-level token (xapp-...) or SLACK_APP_TOKEN env var")
	botToken := fs.String("bot-token", "", "Slack bot token (xoxb-...) or SLACK_BOT_TOKEN env var")
	workspace := fs.String("workspace", "", "workspace name for directory [required]")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *appToken == "" {
		*appToken = os.Getenv("SLACK_APP_TOKEN")
	}
	if *botToken == "" {
		*botToken = os.Getenv("SLACK_BOT_TOKEN")
	}
	if *workspace == "" {
		return fmt.Errorf("required flag: -workspace")
	}
	if *appToken == "" {
		return fmt.Errorf("required: -token or SLACK_APP_TOKEN env var")
	}
	if *botToken == "" {
		return fmt.Errorf("required: -bot-token or SLACK_BOT_TOKEN env var")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	api := slack.New(*botToken, slack.OptionAppLevelToken(*appToken))
	client := socketmode.New(api)

	// Build user and channel name caches
	resolver := newSlackResolver(api)
	if err := resolver.load(ctx); err != nil {
		slog.WarnContext(ctx, "failed to preload Slack names (will resolve on-demand)", "error", err)
	}

	go slackEventLoop(ctx, client, resolver, *workspace)

	slog.InfoContext(ctx, "slack listener started", "workspace", *workspace)
	fmt.Printf("Listening for Slack messages (workspace: %s)...\nPress Ctrl+C to stop.\n", *workspace)

	// Handle shutdown signal in a goroutine
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nShutting down...")
		cancel()
	}()

	return client.RunContext(ctx)
}

func slackEventLoop(ctx context.Context, client *socketmode.Client, resolver *slackResolver, workspace string) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-client.Events:
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
				client.Ack(*evt.Request)
				handleSlackEvent(ctx, resolver, workspace, eventsAPIEvent)
			case socketmode.EventTypeErrorBadMessage:
				slog.WarnContext(ctx, "slack: bad message from Slack")
			case socketmode.EventTypeIncomingError:
				slog.ErrorContext(ctx, "slack: incoming error from Slack")
			}
		}
	}
}

func handleSlackEvent(ctx context.Context, resolver *slackResolver, workspace string, evt slackevents.EventsAPIEvent) {
	switch innerEvt := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		handleSlackMessage(ctx, resolver, workspace, innerEvt)
	}
}

func handleSlackMessage(ctx context.Context, resolver *slackResolver, workspace string, msg *slackevents.MessageEvent) {
	// Skip bot messages
	if msg.BotID != "" {
		return
	}
	// Skip message subtypes (edits, deletes, joins, etc.) — only new messages
	if msg.SubType != "" {
		return
	}
	if msg.Text == "" {
		return
	}

	userName := resolver.userName(ctx, msg.User)
	channelName := resolver.channelName(ctx, msg.Channel)

	// Parse Slack timestamp to time.Time
	ts := parseSlackTimestamp(msg.TimeStamp)

	if err := store.WriteMessage("slack", workspace, channelName, userName, msg.Text, ts); err != nil {
		slog.ErrorContext(ctx, "failed to write slack message", "error", err)
		return
	}

	slog.InfoContext(ctx, "slack message saved",
		"from", userName, "channel", channelName, "text_len", len(msg.Text))
}

func parseSlackTimestamp(ts string) time.Time {
	// Slack timestamps are Unix epoch with microseconds: "1234567890.123456"
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(sec, 0)
}

// slackResolver caches user and channel name lookups.
type slackResolver struct {
	api      *slack.Client
	mu       sync.RWMutex
	users    map[string]string // user ID → display name
	channels map[string]string // channel ID → name
}

func newSlackResolver(api *slack.Client) *slackResolver {
	return &slackResolver{
		api:      api,
		users:    make(map[string]string),
		channels: make(map[string]string),
	}
}

func (r *slackResolver) load(ctx context.Context) error {
	users, err := r.api.GetUsersContext(ctx)
	if err != nil {
		return fmt.Errorf("get users: %w", err)
	}
	r.mu.Lock()
	for _, u := range users {
		name := u.Profile.DisplayName
		if name == "" {
			name = u.RealName
		}
		if name == "" {
			name = u.Name
		}
		r.users[u.ID] = name
	}
	r.mu.Unlock()

	channels, _, err := r.api.GetConversationsContext(ctx, &slack.GetConversationsParameters{
		Types:           []string{"public_channel", "private_channel", "mpim", "im"},
		ExcludeArchived: true,
		Limit:           1000,
	})
	if err != nil {
		return fmt.Errorf("get conversations: %w", err)
	}
	r.mu.Lock()
	for _, ch := range channels {
		name := formatChannelName(ch)
		r.channels[ch.ID] = name
	}
	r.mu.Unlock()

	slog.InfoContext(ctx, "slack resolver loaded", "users", len(r.users), "channels", len(r.channels))
	return nil
}

func (r *slackResolver) userName(ctx context.Context, userID string) string {
	r.mu.RLock()
	name, ok := r.users[userID]
	r.mu.RUnlock()
	if ok {
		return name
	}

	// Cache miss — fetch from API
	user, err := r.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve slack user", "user_id", userID, "error", err)
		return userID
	}
	name = user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}
	r.mu.Lock()
	r.users[userID] = name
	r.mu.Unlock()
	return name
}

func (r *slackResolver) channelName(ctx context.Context, channelID string) string {
	r.mu.RLock()
	name, ok := r.channels[channelID]
	r.mu.RUnlock()
	if ok {
		return name
	}

	// Cache miss — fetch from API
	ch, err := r.api.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve slack channel", "channel_id", channelID, "error", err)
		return channelID
	}
	name = formatChannelName(*ch)
	r.mu.Lock()
	r.channels[channelID] = name
	r.mu.Unlock()
	return name
}

func formatChannelName(ch slack.Channel) string {
	if ch.IsIM {
		return "@" + ch.User
	}
	if ch.IsMpIM {
		return "@" + ch.NameNormalized
	}
	return "#" + ch.NameNormalized
}
