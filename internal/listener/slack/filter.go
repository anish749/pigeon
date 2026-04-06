package slack

import (
	"context"
	"fmt"
)

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
// resolves via the Resolver; for bots it uses the Username field, then falls
// back to an API lookup. Returns an error if no readable name can be resolved.
func resolveSender(ctx context.Context, r *Resolver, userID, botID, username string) (string, string, error) {
	if userID != "" {
		return r.UserName(ctx, userID), userID, nil
	}
	if username != "" {
		return username, botID, nil
	}
	if botID != "" {
		name, err := r.BotName(ctx, botID)
		if err != nil {
			return "", "", err
		}
		return name, botID, nil
	}
	return "", "", fmt.Errorf("message has no user, bot, or username")
}
