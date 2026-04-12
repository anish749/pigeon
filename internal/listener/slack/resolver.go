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

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// mentionRe matches Slack user mentions: <@U12345678> or <@U12345678|displayname>
var mentionRe = regexp.MustCompile(`<@(U[A-Z0-9]+)(?:\|[^>]*)?>`)

// channelMentionRe matches Slack channel mentions: <#C12345678|channel-name>
var channelMentionRe = regexp.MustCompile(`<#(C[A-Z0-9]+)\|([^>]+)>`)

// ResolveText replaces Slack markup in message text with human-readable names.
// Converts <@U12345678> to @displayname and <#C12345678|name> to #name.
func (r *Resolver) ResolveText(ctx context.Context, text string) (string, error) {
	var resolveErr error
	text = mentionRe.ReplaceAllStringFunc(text, func(match string) string {
		if resolveErr != nil {
			return match
		}
		sub := mentionRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name, err := r.UserName(ctx, sub[1])
		if err != nil {
			resolveErr = err
			return match
		}
		return "@" + name
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	text = channelMentionRe.ReplaceAllString(text, "#$2")
	return text, nil
}

// Resolver resolves Slack user and channel names. User lookups are backed
// by the per-workspace identity writer — both hot-path ID lookups (UserName)
// and name-based searches (FindUserID) hit only this workspace's own file,
// since a Slack user ID only ever lives in the workspace that discovered it.
// Channel and membership state is cached locally.
type Resolver struct {
	api       *goslack.Client
	writer    *identity.Writer
	workspace string
	mu        sync.RWMutex
	channels  map[string]string // channel ID → name
	dmUsers   map[string]string // channel ID → DM partner's user ID
	members   map[string]bool   // channel IDs the user has joined
}

// NewResolver creates a new Slack name resolver backed by the per-workspace
// identity writer.
func NewResolver(api *goslack.Client, writer *identity.Writer, workspace string) *Resolver {
	return &Resolver{
		api:       api,
		writer:    writer,
		workspace: workspace,
		channels:  make(map[string]string),
		dmUsers:   make(map[string]string),
		members:   make(map[string]bool),
	}
}

// Load fetches user profiles and channel lists from the Slack API. User
// profiles are pushed to the identity service as signals. Channel names
// are cached locally. Returns the number of users and channels loaded.
func (r *Resolver) Load(ctx context.Context) (users int, channels int, err error) {
	userList, err := r.api.GetUsersContext(ctx)
	if err != nil {
		return 0, 0, err
	}

	// Push all user profiles to identity as a single batch.
	signals := make([]identity.Signal, 0, len(userList))
	for _, u := range userList {
		if u.Deleted {
			continue
		}
		signals = append(signals, r.createSignal(u))
	}
	if err := r.writer.ObserveBatch(signals); err != nil {
		return 0, 0, fmt.Errorf("observe Slack users: %w", err)
	}

	chanList, _, err := r.api.GetConversationsContext(ctx, &goslack.GetConversationsParameters{
		Types:           []string{"public_channel", "private_channel", "mpim", "im"},
		ExcludeArchived: true,
		Limit:           1000,
	})
	if err != nil {
		return len(signals), 0, err
	}
	r.mu.Lock()
	for _, ch := range chanList {
		name := FormatChannelName(ch)
		if ch.IsIM {
			// Resolve IM user ID to display name via identity.
			if userName, err := r.UserName(ctx, ch.User); err == nil {
				name = "@" + userName
			}
		}
		r.channels[ch.ID] = name
	}
	r.mu.Unlock()

	return len(signals), len(chanList), nil
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
func (r *Resolver) RegisterConversation(ctx context.Context, ch goslack.Channel) error {
	name := FormatChannelName(ch)
	if ch.IsIM {
		userName, err := r.UserName(ctx, ch.User)
		if err != nil {
			return fmt.Errorf("resolve IM user %s: %w", ch.User, err)
		}
		name = "@" + userName
		r.mu.Lock()
		r.dmUsers[ch.ID] = ch.User
		r.mu.Unlock()
	}
	r.RegisterChannel(ch.ID, name)
	return nil
}

// UserName resolves a Slack user ID to a display name. Looks up the identity
// service first; on miss, falls back to the Slack API and pushes the result
// as an identity signal for future lookups.
func (r *Resolver) UserName(ctx context.Context, userID string) (string, error) {
	// Check identity service first.
	person, err := r.writer.LookupBySlackID(r.workspace, userID)
	if err != nil {
		slog.WarnContext(ctx, "identity lookup failed, falling back to API",
			"user_id", userID, "error", err)
	}
	if person != nil {
		return person.Name, nil
	}

	// Cache miss — fetch from Slack API and push signal.
	user, err := r.api.GetUserInfoContext(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("resolve user %s: %w", userID, err)
	}

	sig := r.createSignal(*user)
	if err := r.writer.Observe(sig); err != nil {
		slog.ErrorContext(ctx, "identity observe failed after API lookup",
			"user_id", userID, "error", err)
	}

	return sig.Name, nil
}

// botName resolves a Slack bot ID to a display name. Checks identity first,
// then falls back to the bot API and pushes a signal.
func (r *Resolver) botName(ctx context.Context, botID string) (string, error) {
	// Check identity service first.
	person, err := r.writer.LookupBySlackID(r.workspace, botID)
	if err != nil {
		slog.WarnContext(ctx, "identity lookup failed for bot, falling back to API",
			"bot_id", botID, "error", err)
	}
	if person != nil {
		return person.Name, nil
	}

	// Cache miss — fetch from Slack API and push signal.
	bot, err := r.api.GetBotInfoContext(ctx, goslack.GetBotInfoParameters{Bot: botID})
	if err != nil {
		return "", fmt.Errorf("resolve bot %s: %w", botID, err)
	}
	if bot.Name == "" {
		return "", fmt.Errorf("resolve bot %s: API returned empty name", botID)
	}

	sig := r.createBotSignal(botID, bot)
	if err := r.writer.Observe(sig); err != nil {
		slog.ErrorContext(ctx, "identity observe failed for bot",
			"bot_id", botID, "error", err)
	}

	return sig.Name, nil
}

// SenderName resolves a message sender to (name, id). Tries the user ID first,
// then the message's Username field (common for bots), then a bot API lookup.
func (r *Resolver) SenderName(ctx context.Context, userID, botID, username string) (string, string, error) {
	if userID != "" {
		name, err := r.UserName(ctx, userID)
		if err != nil {
			return "", "", err
		}
		return name, userID, nil
	}
	if username != "" {
		return username, botID, nil
	}
	if botID != "" {
		name, err := r.botName(ctx, botID)
		if err != nil {
			return "", "", err
		}
		return name, botID, nil
	}
	return "", "", fmt.Errorf("message has no user, bot, or username")
}

// ChannelName resolves a Slack channel ID to a formatted name. Falls back to API lookup on cache miss.
func (r *Resolver) ChannelName(ctx context.Context, channelID string) (string, error) {
	r.mu.RLock()
	name, ok := r.channels[channelID]
	r.mu.RUnlock()
	if ok {
		return name, nil
	}

	ch, err := r.api.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return "", fmt.Errorf("resolve channel %s: %w", channelID, err)
	}
	name = FormatChannelName(*ch)
	if ch.IsIM {
		userName, err := r.UserName(ctx, ch.User)
		if err != nil {
			return "", err
		}
		name = "@" + userName
	}
	r.mu.Lock()
	r.channels[channelID] = name
	if ch.IsIM {
		r.dmUsers[channelID] = ch.User
	}
	r.mu.Unlock()
	return name, nil
}

// UserMatch represents a user that matched a search query.
type UserMatch struct {
	ID          string
	DisplayName string
	RealName    string
	Email       string
}

// AmbiguousUserError is returned when a user query matches multiple users.
type AmbiguousUserError struct {
	Query   string
	Matches []UserMatch
}

func (e *AmbiguousUserError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "multiple users match %q:\n", e.Query)
	for _, m := range e.Matches {
		fmt.Fprintf(&b, "  %s  %s", m.ID, m.DisplayName)
		if m.RealName != "" && m.RealName != m.DisplayName {
			fmt.Fprintf(&b, "  (%s)", m.RealName)
		}
		if m.Email != "" {
			fmt.Fprintf(&b, "  <%s>", m.Email)
		}
		b.WriteString("\n")
	}
	b.WriteString("ask the user to confirm which person to send to, then use their user ID")
	return b.String()
}

// FindUserID searches identity for a user matching the query.
// Accepts exact user IDs (U...), or case-insensitive substring matches on
// display name, real name, or username. Strips leading @ if present.
func (r *Resolver) FindUserID(query string) (string, string, error) {
	candidates, err := r.writer.SearchCandidates(query)
	if err != nil {
		return "", "", fmt.Errorf("search identity: %w", err)
	}

	// Filter to people with a Slack identity in this workspace.
	var matches []UserMatch
	for _, p := range candidates {
		ws, ok := p.Slack[r.workspace]
		if !ok {
			continue
		}
		var email string
		if len(p.Email) > 0 {
			email = p.Email[0]
		}
		matches = append(matches, UserMatch{
			ID:          ws.ID,
			DisplayName: ws.DisplayName,
			RealName:    ws.RealName,
			Email:       email,
		})
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no user matching %q", query)
	case 1:
		return matches[0].ID, matches[0].DisplayName, nil
	default:
		return "", "", &AmbiguousUserError{Query: query, Matches: matches}
	}
}

// FindChannelID resolves a channel for sending. Requires an exact match:
// either a channel ID (e.g. "D1234567890") or an exact channel name
// (e.g. "#engineering", "@Jeremiah Lu"). Case-insensitive but no substring matching.
// Falls back to the Slack API for channel IDs not yet in the cache.
func (r *Resolver) FindChannelID(ctx context.Context, query string) (string, string, error) {
	r.mu.RLock()

	// Exact channel ID match.
	if name, ok := r.channels[query]; ok {
		r.mu.RUnlock()
		return query, name, nil
	}

	// Exact name match (case-insensitive), with and without prefix.
	q := strings.ToLower(query)
	for id, name := range r.channels {
		lower := strings.ToLower(name)
		if lower == q {
			r.mu.RUnlock()
			return id, name, nil
		}
		// Match without prefix: "engineering" matches "#engineering"
		if len(lower) > 0 && (lower[0] == '#' || lower[0] == '@') && lower[1:] == q {
			r.mu.RUnlock()
			return id, name, nil
		}
	}
	r.mu.RUnlock()

	// API fallback: try looking up the query as a channel ID directly.
	// This handles channels that exist on disk but weren't in the last sync.
	ch, err := r.api.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
		ChannelID: query,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to resolve slack channel", "query", query, "error", err)
		return "", "", fmt.Errorf("no channel matching %q — use the exact channel or contact name from 'pigeon list'", query)
	}
	name := FormatChannelName(*ch)
	if ch.IsIM {
		userName, err := r.UserName(ctx, ch.User)
		if err != nil {
			return "", "", err
		}
		name = "@" + userName
	}
	r.RegisterChannel(query, name)
	return query, name, nil
}

// ConvMeta builds a ConvMeta for the given channel from cached resolver state.
func (r *Resolver) ConvMeta(channelID, channelName string) modelv1.ConvMeta {
	r.mu.RLock()
	userID := r.dmUsers[channelID]
	r.mu.RUnlock()

	switch {
	case userID != "":
		return modelv1.NewSlackDM(channelName, channelID, userID)
	case strings.HasPrefix(channelName, "@mpdm-"):
		return modelv1.NewSlackGroupDM(channelName, channelID)
	case strings.HasPrefix(channelName, "@"):
		return modelv1.NewSlackDM(channelName, channelID, "")
	default:
		return modelv1.NewSlackChannel(channelName, channelID)
	}
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

// createSignal builds an identity signal from a Slack user API response.
func (r *Resolver) createSignal(u goslack.User) identity.Signal { //nolint:gocritic // value receiver is fine for signal construction
	name := u.Profile.DisplayName
	if name == "" {
		name = u.RealName
	}
	if name == "" {
		name = u.Name
	}
	return identity.Signal{
		Email: u.Profile.Email,
		Name:  name,
		Slack: &identity.SlackIdentity{
			Workspace:   r.workspace,
			ID:          u.ID,
			DisplayName: u.Profile.DisplayName,
			RealName:    u.RealName,
			Name:        u.Name,
		},
	}
}

// createBotSignal builds an identity signal from a Slack bot API response.
func (r *Resolver) createBotSignal(botID string, bot *goslack.Bot) identity.Signal {
	return identity.Signal{
		Name: bot.Name,
		Slack: &identity.SlackIdentity{
			Workspace:   r.workspace,
			ID:          botID,
			DisplayName: bot.Name,
		},
	}
}
