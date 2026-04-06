package slack

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
