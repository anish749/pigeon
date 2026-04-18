package modelv1

import (
	"encoding/json"
	"testing"
)

func TestFormatRaw_EmptyMap(t *testing.T) {
	lines := formatRaw(nil, "    ")
	if lines != nil {
		t.Errorf("expected nil, got %v", lines)
	}
}

func TestFormatRaw_ReturnsJSON(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{"fallback": "Bug created", "title": "BUG-1"},
		},
	}
	lines := formatRaw(raw, "    ")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	// Verify it's valid JSON after trimming indent.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0][4:]), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	atts, ok := parsed["attachments"].([]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("attachments = %v, want slice of 1", parsed["attachments"])
	}
}

func TestFormatRaw_Indented(t *testing.T) {
	raw := map[string]any{"files": []any{map[string]any{"name": "doc.pdf"}}}
	lines := formatRaw(raw, "  ")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0][:2] != "  " {
		t.Errorf("expected indent, got %q", lines[0][:2])
	}
}
