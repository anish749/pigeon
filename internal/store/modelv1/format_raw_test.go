package modelv1

import (
	"strings"
	"testing"
	"time"
)

func TestFormatMsg_Attachment(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "jira", SenderID: "B04D", Text: "",
			Raw: map[string]any{
				"attachments": []any{
					map[string]any{
						"fallback": "Alice created Bug <https://jira.example.com/browse/BUG-1|BUG-1>",
						"fields": []any{
							map[string]any{"title": "Assignee", "value": "Alice"},
							map[string]any{"title": "Priority", "value": "Major (P2)"},
						},
					},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if !strings.Contains(lines[1], "📎") {
		t.Errorf("expected attachment prefix, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "BUG-1") {
		t.Errorf("expected fallback text, got %q", lines[1])
	}
	if !strings.Contains(lines[2], "Assignee: Alice") {
		t.Errorf("expected fields, got %q", lines[2])
	}
	if !strings.Contains(lines[2], "Priority: Major (P2)") {
		t.Errorf("expected priority field, got %q", lines[2])
	}
}

func TestFormatMsg_File(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "check this screenshot",
			Raw: map[string]any{
				"files": []any{
					map[string]any{
						"name":      "screenshot.png",
						"mimetype":  "image/png",
						"size":      float64(197770),
						"permalink": "https://example.slack.com/files/U1/F0AE/screenshot.png",
					},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[1], "📄") {
		t.Errorf("expected file prefix, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "screenshot.png") {
		t.Errorf("expected filename, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "image/png") {
		t.Errorf("expected mimetype, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "193.1KB") {
		t.Errorf("expected human size, got %q", lines[1])
	}
}

func TestFormatMsg_FilePermalink(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "here",
			Raw: map[string]any{
				"files": []any{
					map[string]any{
						"name":      "doc.pdf",
						"mimetype":  "application/pdf",
						"size":      float64(1048576),
						"permalink": "https://example.slack.com/files/U1/F1/doc.pdf",
					},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "https://example.slack.com/files/U1/F1/doc.pdf") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected permalink in output, got %v", lines)
	}
}

func TestFormatMsg_NoRaw(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "plain text",
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 1 {
		t.Errorf("plain text message should be 1 line, got %d", len(lines))
	}
}

func TestFormatMsg_AttachmentAndFile(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "see below",
			Raw: map[string]any{
				"attachments": []any{
					map[string]any{"fallback": "JIRA link preview"},
				},
				"files": []any{
					map[string]any{"name": "screenshot.png", "mimetype": "image/png", "size": float64(5000)},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	hasAttach := false
	hasFile := false
	for _, l := range lines {
		if strings.Contains(l, "📎") {
			hasAttach = true
		}
		if strings.Contains(l, "📄") {
			hasFile = true
		}
	}
	if !hasAttach {
		t.Error("expected attachment line")
	}
	if !hasFile {
		t.Error("expected file line")
	}
}

func TestFormatMsg_ReplyWithRaw(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "R1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true,
			Raw: map[string]any{
				"files": []any{
					map[string]any{"name": "img.jpg", "mimetype": "image/jpeg", "size": float64(2048)},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) < 2 {
		t.Fatalf("lines = %d, want >= 2", len(lines))
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("reply line should be indented, got %q", l)
		}
	}
}

func TestFormatRawAttachments_DuplicateFieldSkipped(t *testing.T) {
	// Jenkins pattern: fallback and field value are identical.
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
	lines := formatRaw(raw, "    ")
	// Should have only the fallback line, no field line.
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1 (duplicate field should be skipped)", len(lines))
	}
}

func TestFormatRawAttachments_NoPreviewSkipped(t *testing.T) {
	raw := map[string]any{
		"attachments": []any{
			map[string]any{"fallback": "[no preview available]"},
		},
	}
	lines := formatRaw(raw, "    ")
	if len(lines) != 0 {
		t.Errorf("expected no lines for [no preview available], got %v", lines)
	}
}

func TestFormatRaw_NilMap(t *testing.T) {
	lines := formatRaw(nil, "    ")
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

