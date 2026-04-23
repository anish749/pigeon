package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"time"

	goslack "github.com/slack-go/slack"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/listener/slack/slackerr"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// mentionRe matches Slack user mentions: <@U12345678> or <@U12345678|displayname>
var mentionRe = regexp.MustCompile(`<@(U[A-Z0-9]+)(?:\|[^>]*)?>`)

// channelMentionRe matches Slack channel mentions: <#C12345678|channel-name>
var channelMentionRe = regexp.MustCompile(`<#(C[A-Z0-9]+)\|([^>]+)>`)

// outboundMentionRe matches @name patterns in outbound message text.
// Captures one or more words after @, including Unicode letters (e.g. Björk, Ørjan).
// Does not match inside URLs or emails.
var outboundMentionRe = regexp.MustCompile(`(?:^|\s)@([\pL\pN][\pL\pN_.-]*)`)

// specialMentions maps broadcast @mentions to Slack's special syntax.
var specialMentions = map[string]string{
	"channel":  "<!channel>",
	"here":     "<!here>",
	"everyone": "<!everyone>",
}

// specialMentionRes is a precompiled regex for each special mention.
var specialMentionRes = func() map[string]*regexp.Regexp {
	m := make(map[string]*regexp.Regexp, len(specialMentions))
	for name := range specialMentions {
		m[name] = regexp.MustCompile(`(?:^|\s)@` + regexp.QuoteMeta(name) + `\b`)
	}
	return m
}()

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

// ResolveMentions converts human-readable @mentions in outbound message text
// to Slack's wire format. Replaces @name with <@USER_ID> when exactly one user
// matches, and @channel/@here/@everyone with <!channel>/<!here>/<!everyone>.
// Unrecognized or ambiguous mentions are left as-is.
//
// Multi-word names (e.g. "@Sherlock Holmes") are handled by greedy lookahead:
// the regex captures the first word, SearchCandidates returns all substring
// matches, and we pick the candidate whose full name appears longest in the
// text starting at the @ position.
func (r *Resolver) ResolveMentions(text string) string {
	// Handle special broadcast mentions first.
	for name, repl := range specialMentions {
		pat := specialMentionRes[name]
		text = pat.ReplaceAllStringFunc(text, func(m string) string {
			prefix := ""
			if len(m) > 0 && m[0] != '@' {
				prefix = string(m[0])
			}
			return prefix + repl
		})
	}

	// Resolve user mentions. Process from right to left so replacements
	// don't shift indices for earlier matches.
	matches := outboundMentionRe.FindAllStringSubmatchIndex(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		// m[2]:m[3] is the captured name (group 1).
		name := text[m[2]:m[3]]

		// Skip if already handled as a special mention.
		if _, ok := specialMentions[strings.ToLower(name)]; ok {
			continue
		}

		candidates, err := r.writer.SearchCandidates(name)
		if err != nil {
			slog.Warn("mention search failed", "mention", name, "error", err)
			continue
		}

		// Filter to this workspace and find the candidate whose name
		// matches the longest span of text starting at the @. Try all
		// name fields (canonical name, display name, real name, username)
		// so that both "@alice" and "@Sherlock Holmes" resolve correctly.
		// A unique username match (e.g. "@sherlock" → username "sherlock")
		// is treated as unambiguous even when multiple candidates share
		// the first name, since usernames are unique handles in Slack.
		atIdx := m[2] - 1
		afterAt := text[atIdx+1:] // text after the @
		var bestID string
		var bestLen int
		var ties int
		for _, p := range candidates {
			ws, ok := p.Slack[r.workspace]
			if !ok {
				continue
			}
			names := uniqueNames(p.Name, ws.DisplayName, ws.RealName, ws.Name)
			for _, n := range names {
				if len(n) > len(afterAt) {
					continue
				}
				if !strings.EqualFold(afterAt[:len(n)], n) {
					continue
				}
				// Ensure the match ends at a word boundary: end of string,
				// or followed by a non-letter/digit (Unicode-aware).
				if len(n) < len(afterAt) {
					ch, _ := utf8.DecodeRuneInString(afterAt[len(n):])
					if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
						continue
					}
				}
				if len(n) > bestLen {
					bestID = ws.ID
					bestLen = len(n)
					ties = 1
				} else if len(n) == bestLen && ws.ID != bestID {
					ties++
				}
			}
		}

		if bestID == "" || ties > 1 {
			if bestID == "" {
				slog.Warn("mention not resolved, leaving as-is", "mention", name,
					"error", fmt.Errorf("no user matching %q", name))
			} else {
				slog.Warn("mention not resolved, leaving as-is", "mention", name,
					"error", fmt.Errorf("multiple users match %q at same length", name))
			}
			continue
		}

		// Replace @<matched name> with <@USER_ID>.
		endIdx := atIdx + 1 + bestLen
		text = text[:atIdx] + "<@" + bestID + ">" + text[endIdx:]
	}

	return text
}

// uniqueNames returns the non-empty, deduplicated names from the given list.
func uniqueNames(names ...string) []string {
	seen := make(map[string]bool, len(names))
	var out []string
	for _, n := range names {
		if n == "" {
			continue
		}
		lower := strings.ToLower(n)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, n)
	}
	return out
}

// Resolver resolves Slack user and channel names. User lookups are backed
// by the per-workspace identity writer — both hot-path ID lookups (UserName)
// and name-based searches (FindUserID) hit only this workspace's own file,
// since a Slack user ID only ever lives in the workspace that discovered it.
// Channel and membership state is cached locally.
type Resolver struct {
	api        *goslack.Client
	botAPI     *goslack.Client // used as a fallback when the user token cannot see a channel (e.g. bot↔other-user DMs).
	writer     *identity.Writer
	workspace  string
	mu         sync.RWMutex
	channels   map[string]string // channel ID → name
	dmUsers    map[string]string // channel ID → DM partner's user ID
	members    map[string]bool   // channel IDs the user has joined
	botMembers map[string]bool   // channel IDs the bot has joined
}

// NewResolver creates a new Slack name resolver backed by the per-workspace
// identity writer. botAPI is used as a fallback when the user token returns
// channel_not_found — typically for DMs the user is not a party to.
func NewResolver(api, botAPI *goslack.Client, writer *identity.Writer, workspace string) *Resolver {
	return &Resolver{
		api:        api,
		botAPI:     botAPI,
		writer:     writer,
		workspace:  workspace,
		channels:   make(map[string]string),
		dmUsers:    make(map[string]string),
		members:    make(map[string]bool),
		botMembers: make(map[string]bool),
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

// AddBotMember marks a channel as one the bot has joined.
func (r *Resolver) AddBotMember(channelID string) {
	r.mu.Lock()
	r.botMembers[channelID] = true
	r.mu.Unlock()
}

// RemoveBotMember marks a channel as one the bot has left.
func (r *Resolver) RemoveBotMember(channelID string) {
	r.mu.Lock()
	delete(r.botMembers, channelID)
	r.mu.Unlock()
}

// IsBotMember reports whether the bot is a member of the given channel.
// Populated from the bot-token conversations.list sweep during sync and kept
// in sync by member_joined/left_channel events scoped to the bot's user ID.
func (r *Resolver) IsBotMember(channelID string) bool {
	r.mu.RLock()
	ok := r.botMembers[channelID]
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

// BotName resolves a Slack bot ID to a display name. Checks identity first,
// then falls back to the bot API and pushes a signal.
func (r *Resolver) BotName(ctx context.Context, botID string) (string, error) {
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

// ResolvedSender holds the resolver-derived fields common to all incoming
// Slack events (messages, reactions, edits, deletes).
type ResolvedSender struct {
	ChannelName string
	SenderName  string
	SenderID    string
}

// ResolveSender resolves both the channel and sender for an incoming event.
func (r *Resolver) ResolveSender(ctx context.Context, channelID string, msg goslack.Msg) (ResolvedSender, error) {
	channelName, err := r.ChannelName(ctx, channelID)
	if err != nil {
		return ResolvedSender{}, fmt.Errorf("resolve channel %s: %w", channelID, err)
	}
	senderName, senderID, err := r.SenderName(ctx, msg)
	if err != nil {
		return ResolvedSender{}, err
	}
	return ResolvedSender{ChannelName: channelName, SenderName: senderName, SenderID: senderID}, nil
}

// SenderName resolves a message sender to (name, id). Tries the user ID first,
// then the message's Username field (common for bots), then a bot API lookup.
func (r *Resolver) SenderName(ctx context.Context, msg goslack.Msg) (string, string, error) {
	if msg.User != "" {
		name, err := r.UserName(ctx, msg.User)
		if err != nil {
			return "", "", err
		}
		return name, msg.User, nil
	}
	if msg.Username != "" {
		return msg.Username, msg.BotID, nil
	}
	if msg.BotID != "" {
		name, err := r.BotName(ctx, msg.BotID)
		if err != nil {
			return "", "", err
		}
		return name, msg.BotID, nil
	}
	return "", "", fmt.Errorf("message has no user, bot, or username")
}

// ChannelName resolves a Slack channel ID to a formatted name. Falls back to API lookup on cache miss.
// If the user token returns channel_not_found (e.g. a DM between the bot and
// another user, where the pigeon user is not a party), retries via the bot token.
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
		if !slackerr.IsChannelNotFound(err) {
			return "", fmt.Errorf("resolve channel %s: %w", channelID, err)
		}
		ch, err = r.botAPI.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
			ChannelID: channelID,
		})
		if err != nil {
			return "", fmt.Errorf("resolve channel %s via bot: %w", channelID, err)
		}
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
	// If the user token returns channel_not_found, retry via the bot token —
	// the bot is a party to DMs the pigeon user isn't.
	ch, err := r.api.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
		ChannelID: query,
	})
	if err != nil && slackerr.IsChannelNotFound(err) {
		ch, err = r.botAPI.GetConversationInfoContext(ctx, &goslack.GetConversationInfoInput{
			ChannelID: query,
		})
	}
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
