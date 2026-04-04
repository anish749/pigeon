package hub

type NotificationMsg interface {
	Content() string
	Meta() map[string]any
}

var _ NotificationMsg = (*TextNotificationMsg)(nil)

type TextNotificationMsg struct {
	Text string
}

func (t *TextNotificationMsg) Content() string {
	return t.Text
}

func (t *TextNotificationMsg) Meta() map[string]any {
	return map[string]any{}
}
