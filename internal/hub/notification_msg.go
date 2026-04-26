package hub

// NotificationMsg is the payload Session.Send accepts when the hub
// delivers a frame to a connected MCP session. It abstracts over both
// channel data (formatted message lines) and out-of-band session
// control text (takeover notices, lifecycle errors) so a single Send
// path covers every shape the session might receive.
type NotificationMsg interface {
	Content() string
	Meta() map[string]any
}

var _ NotificationMsg = (*TextNotificationMsg)(nil)

// TextNotificationMsg carries a plain string with no metadata tags —
// used for session-control frames where the text is the whole signal.
type TextNotificationMsg struct {
	Text string
}

func (t *TextNotificationMsg) Content() string {
	return t.Text
}

func (t *TextNotificationMsg) Meta() map[string]any {
	return map[string]any{}
}
