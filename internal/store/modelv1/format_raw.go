package modelv1

import (
	"encoding/json"
)

// formatRaw serializes raw platform content as a JSON line. AI agent
// consumers parse JSON natively, so no platform-specific formatting is
// needed — the raw data is strictly more informative than any summary.
func formatRaw(raw map[string]any, indent string) []string {
	if len(raw) == 0 {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	return []string{indent + string(data)}
}
