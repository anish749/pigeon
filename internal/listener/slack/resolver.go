package slack

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	goslack "github.com/slack-go/slack"
)

// mentionRe matches Slack user mentions: <@U12345678> or <@U12345678|displayname>
var mentionRe = regexp.MustCompile(`<@(U[A-Z0-9]+)(?:\|[^>]*)?>`)

// channelMentionRe matches Slack channel mentions: <#C12345678|channel-name>
var channelMentionRe = regexp.MustCompile(`<#(C[A-Z0-9]+)\|([^>]+)>`)

// ResolveText replaces Slack markup in message text with human-readable names.
// Converts <@U12345678> to @displayname and <#C12345678|name> to #name.
func (r *Resolver) ResolveText(ctx context.Context, text string) string {
	text = mentionRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := mentionRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return "@" + r.UserName(ctx, sub[1])
	})
	text = channelMentionRe.ReplaceAllString(text, "#$2")
	return text
}

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
		name := FormatChannelName(ch)
		// Resolve IM user IDs to display names (we already hold the lock
		// so read r.users directly instead of calling UserName).
		if ch.IsIM {
			if userName, ok := r.users[ch.User]; ok {
				name = "@" + userName
			}
		}
		r.channels[ch.ID] = name
	}
	r.mu.Unlock()

	return len(r.users), len(r.channels), nil
}

// RegisterChannel adds a channel name to the cache.
func (r *Resolver) RegisterChannel(channelID, name string) {
	r.mu.Lock()
	r.channels[channelID] = name
	r.mu.Unlock()
}

// RegisterConversation registers a channel in the cache, resolving IM user IDs
// to display names. Used by sync to register channels discovered via the user token.
func (r *Resolver) RegisterConversation(ctx context.Context, ch goslack.Channel) {
	name := FormatChannelName(ch)
	if ch.IsIM {
		name = "@" + r.UserName(ctx, ch.User)
	}
	r.RegisterChannel(ch.ID, name)
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
	if ch.IsIM {
		name = "@" + r.UserName(ctx, ch.User)
	}
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
