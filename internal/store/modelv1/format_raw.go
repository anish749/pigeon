package modelv1

import (
	"github.com/anish749/pigeon/internal/store/modelv1/slackraw"
)

// RawFormatter formats platform-specific raw content for display.
type RawFormatter interface {
	FormatRaw(raw map[string]any, indent string) []string
}

var rawFormatters = map[RawType]RawFormatter{
	RawTypeSlack: &slackraw.Formatter{},
}

// formatRaw looks up the appropriate RawFormatter for the given raw type
// and delegates rendering. Returns nil if there is no formatter or no
// content to render.
func formatRaw(rawType RawType, raw map[string]any, indent string) []string {
	if len(raw) == 0 {
		return nil
	}
	f, ok := rawFormatters[rawType]
	if !ok {
		return nil
	}
	return f.FormatRaw(raw, indent)
}
