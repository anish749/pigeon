package slack

import (
	"context"
	"fmt"
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

// Resolver caches Slack user and channel name lookups and tracks
// which channels the authenticated user is a member of.
type Resolver struct {
	api      *goslack.Client
	mu       sync.RWMutex
	users    map[string]string // user ID → display name
	channels map[string]string // channel ID → name
	members  map[string]bool   // channel IDs the user has joined
}

// NewResolver creates a new Slack name resolver.
func NewResolver(api *goslack.Client) *Resolver {
	return &Resolver{
		api:      api,
		users:    make(map[string]string),
		channels: make(map[string]string),
		members:  make(map[string]bool),
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

// AddMember marks a channel as one the user has joined.
func (r *Resolver) AddMember(channelID string) {
	r.mu.Lock()
	r.members[channelID] = true
	r.mu.Unlock()
}

// RemoveMember marks a channel as one the user has left.
func (r *Resolver) RemoveMember(channelID string) {
	r.mu.Lock()
	delete(r.members, channelID)
	r.mu.Unlock()
}

// IsMember reports whether the user is a member of the given channel.
func (r *Resolver) IsMember(channelID string) bool {
	r.mu.RLock()
	ok := r.members[channelID]
	r.mu.RUnlock()
	return ok
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

// UserMatch represents a user that matched a search query.
type UserMatch struct {
	ID   string
	Name string
}

// AmbiguousUserError is returned when a user query matches multiple users.
type AmbiguousUserError struct {
	Query   string
	Matches []UserMatch
}

func (e *AmbiguousUserError) Error() string {
	return fmt.Sprintf("multiple users match %q (%d matches)", e.Query, len(e.Matches))
}

// FindUserID searches the user cache for a user matching the query by display name.
// Accepts case-insensitive substring matches. Strips leading @ if present.
func (r *Resolver) FindUserID(query string) (string, string, error) {
	q := strings.ToLower(strings.TrimPrefix(query, "@"))

	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []UserMatch
	for id, name := range r.users {
		if strings.Contains(strings.ToLower(name), q) {
			matches = append(matches, UserMatch{id, name})
		}
	}

	if len(matches) == 0 {
		return "", "", fmt.Errorf("no user matching %q", query)
	}
	if len(matches) == 1 {
		return matches[0].ID, matches[0].Name, nil
	}
	return "", "", &AmbiguousUserError{Query: query, Matches: matches}
}

// ChannelMatch represents a channel that matched a search query.
type ChannelMatch struct {
	ID   string
	Name string
}

// AmbiguousChannelError is returned when a channel query matches multiple channels.
// The caller should enrich matches with activity info before displaying.
type AmbiguousChannelError struct {
	Query   string
	Matches []ChannelMatch
}

func (e *AmbiguousChannelError) Error() string {
	return fmt.Sprintf("multiple channels match %q (%d matches)", e.Query, len(e.Matches))
}

// FindChannelID searches the channel cache for a channel matching the query.
// Accepts channel IDs directly (e.g. "D1234567890") or matches case-insensitively
// against channel names (with and without prefix).
// Returns AmbiguousChannelError if multiple matches, so the caller can enrich and display.
func (r *Resolver) FindChannelID(query string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Exact channel ID match.
	if name, ok := r.channels[query]; ok {
		return query, name, nil
	}

	q := strings.ToLower(query)
	var matches []ChannelMatch

	for id, name := range r.channels {
		lower := strings.ToLower(name)
		if strings.Contains(lower, q) {
			matches = append(matches, ChannelMatch{id, name})
			continue
		}
		if len(lower) > 0 && (lower[0] == '#' || lower[0] == '@') {
			if strings.Contains(lower[1:], q) {
				matches = append(matches, ChannelMatch{id, name})
			}
		}
	}

	if len(matches) == 0 {
		return "", "", fmt.Errorf("no channel matching %q", query)
	}
	if len(matches) == 1 {
		return matches[0].ID, matches[0].Name, nil
	}

	return "", "", &AmbiguousChannelError{Query: query, Matches: matches}
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
