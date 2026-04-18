package modelv1

import (
	"testing"
)

func TestFormatRaw_UnknownType(t *testing.T) {
	raw := map[string]any{"attachments": []any{map[string]any{"fallback": "test"}}}
	lines := formatRaw("unknown", raw, "    ")
	if lines != nil {
		t.Errorf("expected nil for unknown raw type, got %v", lines)
	}
}

func TestFormatRaw_EmptyType(t *testing.T) {
	raw := map[string]any{"attachments": []any{map[string]any{"fallback": "test"}}}
	lines := formatRaw("", raw, "    ")
	if lines != nil {
		t.Errorf("expected nil for empty raw type, got %v", lines)
	}
}

func TestFormatRaw_NilMap(t *testing.T) {
	lines := formatRaw(RawTypeSlack, nil, "    ")
	if lines != nil {
		t.Errorf("expected nil, got %v", lines)
	}
}
