package slack

import (
	"github.com/slack-go/slack/slackevents"

	"github.com/anish749/pigeon/internal/hub"
)

// shouldAutoReply reports whether a no-session auto-reply should be sent for
// this message. It requires all three conditions:
//   - the message is a DM to the bot (isBotDM)
//   - no pigeon session is configured (RouteNoSession)
//   - the sender is not our own bot, which would create an infinite loop
//     (each auto-reply arrives back as a bot_message event in the same DM,
//     triggering another auto-reply, bounded only by Slack rate limits)
func shouldAutoReply(pigeonBotUID string, msg *slackevents.MessageEvent, routeState hub.RouteState, isBotDM bool) bool {
	if !isBotDM {
		return false
	}
	if routeState != hub.RouteNoSession {
		return false
	}
	if pigeonBotUID != "" && (msg.User == pigeonBotUID || msg.BotID == pigeonBotUID) {
		return false
	}
	return true
}

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
