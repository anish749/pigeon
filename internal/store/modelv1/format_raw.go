package modelv1

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
