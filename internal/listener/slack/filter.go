package slack

import "context"

// allowedSubType returns true if a message with this SubType should be saved.
// Empty subtype (normal message), thread_broadcast, and bot_message are allowed.
// System events like channel_join, channel_topic, channel_leave are filtered.
func allowedSubType(subType string) bool {
	switch subType {
	case "", "thread_broadcast", "bot_message":
		return true
	default:
		return false
	}
}

// resolveSender returns (name, id) for a message sender. For human users it
// resolves via the Resolver; for bots it falls back to the bot's Username
// field or BotID.
func resolveSender(ctx context.Context, r *Resolver, userID, botID, username string) (string, string) {
	if userID != "" {
		return r.UserName(ctx, userID), userID
	}
	if username != "" {
		return username, botID
	}
	if botID != "" {
		return botID, botID
	}
	return "unknown", ""
}
