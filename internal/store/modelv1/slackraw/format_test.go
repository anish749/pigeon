package slackraw

import (
	"strings"
	"testing"
)

func TestFormatRaw_Attachment(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{
				"fallback": "Alice created Bug BUG-1",
				"fields": []any{
					map[string]any{"title": "Assignee", "value": "Alice"},
					map[string]any{"title": "Priority", "value": "Major (P2)"},
				},
			},
		},
	}
	f := &Formatter{}
	lines := f.FormatRaw(raw, "    ")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "📎") {
		t.Errorf("expected attachment prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Assignee: Alice") {
		t.Errorf("expected fields, got %q", lines[1])
	}
}

func TestFormatRaw_DuplicateFieldSkipped(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{
				"fallback": "Deploy initiated",
				"fields": []any{
					map[string]any{"title": "", "value": "Deploy initiated"},
				},
			},
		},
	}
	f := &Formatter{}
	lines := f.FormatRaw(raw, "    ")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (duplicate field should be skipped)", len(lines))
	}
}

func TestFormatRaw_NoPreviewSkipped(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{"fallback": "[no preview available]"},
		},
	}
	f := &Formatter{}
	lines := f.FormatRaw(raw, "    ")
	if len(lines) != 0 {
		t.Errorf("expected no lines for [no preview available], got %v", lines)
	}
}

func TestFormatRaw_File(t *testing.T) {
	raw := map[string]any{
		"files": []any{
			map[string]any{
				"name":      "screenshot.png",
				"mimetype":  "image/png",
				"size":      float64(197770),
				"permalink": "https://example.slack.com/files/U1/F1/screenshot.png",
			},
		},
	}
	f := &Formatter{}
	lines := f.FormatRaw(raw, "    ")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "📄") {
		t.Errorf("expected file prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "screenshot.png") {
		t.Errorf("expected filename, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "193.1KB") {
		t.Errorf("expected human size, got %q", lines[0])
	}
}

func TestFormatRaw_Empty(t *testing.T) {
	f := &Formatter{}
	lines := f.FormatRaw(map[string]any{}, "    ")
	if lines != nil {
		t.Errorf("expected nil, got %v", lines)
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{197770, "193.1KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := humanSize(tt.bytes)
			if got != tt.want {
				t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}
