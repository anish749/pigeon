package slack

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	goslack "github.com/slack-go/slack"
)

// Resolver caches Slack user and channel name lookups.
type Resolver struct {
	api      *goslack.Client
	mu       sync.RWMutex
	users    map[string]string // user ID → display name
	channels map[string]string // channel ID → name
}

// NewResolver creates a new Slack name resolver.
func NewResolver(api *goslack.Client) *Resolver {
	return &Resolver{
		api:      api,
		users:    make(map[string]string),
		channels: make(map[string]string),
	}
}

// Load preloads user and channel name caches from the Slack API.
// Returns the number of users and channels loaded.
func (r *Resolver) Load(ctx context.Context) (users int, channels int, err error) {
	userList, err := r.api.GetUsersContext(ctx)
	if err != nil {
		return 0, 0, err
	}
	r.mu.Lock()
	for _, u := range userList {
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

	chanList, _, err := r.api.GetConversationsContext(ctx, &goslack.GetConversationsParameters{
		Types:           []string{"public_channel", "private_channel", "mpim", "im"},
		ExcludeArchived: true,
		Limit:           1000,
	})
	if err != nil {
		return 0, 0, err
	}
	r.mu.Lock()
	for _, ch := range chanList {
		r.channels[ch.ID] = FormatChannelName(ch)
	}
	r.mu.Unlock()

	return len(r.users), len(r.channels), nil
}

// RegisterChannel adds a channel name to the cache. Used by backfill to register
// channels discovered via the user token that the bot token may not see.
func (r *Resolver) RegisterChannel(channelID, name string) {
	r.mu.Lock()
	r.channels[channelID] = name
	r.mu.Unlock()
}

// UserName resolves a Slack user ID to a display name. Falls back to API lookup on cache miss.
func (r *Resolver) UserName(ctx context.Context, userID string) string {
	r.mu.RLock()
	name, ok := r.users[userID]
	r.mu.RUnlock()
	if ok {
		return name
	}

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

// ChannelName resolves a Slack channel ID to a formatted name. Falls back to API lookup on cache miss.
func (r *Resolver) ChannelName(ctx context.Context, channelID string) string {
	r.mu.RLock()
	name, ok := r.channels[channelID]
	r.mu.RUnlock()
	if ok {
		return name
	}

	ch, err := r.api.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve slack channel", "channel_id", channelID, "error", err)
		return channelID
	}
	name = FormatChannelName(*ch)
	r.mu.Lock()
	r.channels[channelID] = name
	r.mu.Unlock()
	return name
}

// FormatChannelName returns a human-readable channel name with prefix (# for channels, @ for DMs).
func FormatChannelName(ch goslack.Channel) string {
	if ch.IsIM {
		return "@" + ch.User
	}
	if ch.IsMpIM {
		return "@" + ch.NameNormalized
	}
	return "#" + ch.NameNormalized
}

// ParseTimestamp converts a Slack timestamp ("1234567890.123456") to time.Time.
func ParseTimestamp(ts string) time.Time {
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Now()
	}
	return time.Unix(sec, 0)
}
