package slack

// shouldAutoReply reports whether a no-session auto-reply should be sent for
// this message. It returns true only when the message is a DM to the bot from
// someone else — never when the bot itself is the sender, which would create an
// infinite loop (each auto-reply arrives back as a bot_message event in the
// same DM, triggering another auto-reply).
func shouldAutoReply(botUserID, msgUser, msgBotID string, isBotDM bool) bool {
	if !isBotDM {
		return false
	}
	if botUserID != "" && (msgUser == botUserID || msgBotID == botUserID) {
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
