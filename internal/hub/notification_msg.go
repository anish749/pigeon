package hub

type NotificationMsg interface {
	Content() string
	Meta() map[string]any
}

