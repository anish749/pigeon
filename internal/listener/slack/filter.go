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

// shouldKeepMessage reports whether a Slack message should be stored.
// Messages with empty text are always skipped. The subtype determines whether
// the message is conversational content (keep) or a system/structural event (skip).
//
// Known subtypes and their handling:
//
// Kept (conversational content):
//
//	""                    regular message — human-typed or app-posted via chat.postMessage
//	"bot_message"         legacy bot or incoming webhook post (e.g. CI alerts, k8s notifications)
//	"thread_broadcast"    thread reply also posted to the channel ("Also send to #channel")
//	"assistant_app_thread" app assistant conversation (e.g. Slack AI, custom app threads)
//
// Skipped (system/structural events):
//
//	"message_changed"     edit notification — routed to handleEdit in the listener, not present in history
//	"message_deleted"     delete notification — routed to handleDelete in the listener, not present in history
//	"channel_join"        system: user joined channel
//	"channel_leave"       system: user left channel
//	"channel_topic"       system: channel topic changed
//	"channel_purpose"     system: channel purpose changed
//	"channel_name"        system: channel renamed
//	"channel_archive"     system: channel archived
//	"channel_unarchive"   system: channel unarchived
//	"group_join"          system: user joined private channel
//	"group_leave"         system: user left private channel
//	"group_topic"         system: private channel topic changed
//	"group_purpose"       system: private channel purpose changed
//	"group_name"          system: private channel renamed
//	"group_archive"       system: private channel archived
//	"group_unarchive"     system: private channel unarchived
//	"pinned_item"         system: item pinned
//	"unpinned_item"       system: item unpinned
//	"ekm_access_denied"   system: message hidden by EKM
//	"me_message"          /me slash command — could be kept but rare in practice
//	"huddle_thread"       huddle-related system message
//	"tombstone"           placeholder for deleted messages in history
//	"file_share"          file uploaded — has empty text unless user added a comment
func shouldKeepMessage(subType, text string) bool {
	if text == "" {
		return false
	}
	switch subType {
	case "", "bot_message", "thread_broadcast", "assistant_app_thread":
		return true
	default:
		return false
	}
}
