package modelv1

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDisplaySender(t *testing.T) {
	tests := []struct {
		name   string
		sender string
		via    Via
		want   string
	}{
		{"organic", "Alice", ViaOrganic, "Alice"},
		{"pigeon-as-bot", "Anish's Pigeon", ViaPigeonAsBot, "sent by pigeon"},
		{"pigeon-as-user", "Anish Chakraborty", ViaPigeonAsUser, "Anish Chakraborty (via pigeon)"},
		{"to-pigeon", "Jeremiah Lu", ViaToPigeon, "sent to pigeon by Jeremiah Lu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displaySender(tt.sender, tt.via)
			if got != tt.want {
				t.Errorf("displaySender(%q, %q) = %q, want %q", tt.sender, tt.via, got, tt.want)
			}
		})
	}
}

func TestFormatMsg_Via(t *testing.T) {
	tests := []struct {
		name       string
		via        Via
		sender     string
		wantSender string
	}{
		{"pigeon-as-bot", ViaPigeonAsBot, "Anish's Pigeon", "sent by pigeon"},
		{"pigeon-as-user", ViaPigeonAsUser, "Anish", "Anish (via pigeon)"},
		{"to-pigeon", ViaToPigeon, "Jeremiah", "sent to pigeon by Jeremiah"},
		{"organic", ViaOrganic, "Alice", "Alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ResolvedMsg{
				MsgLine: MsgLine{
					ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
					Sender: tt.sender, SenderID: "U1", Text: "hello",
					Via: tt.via,
				},
			}
			lines := FormatMsg(m, time.UTC)
			if !strings.Contains(lines[0], tt.wantSender+" (U1)") {
				t.Errorf("got %q, want sender %q", lines[0], tt.wantSender)
			}
		})
	}
}

func TestNotificationFormat_ViaSenderDecoration(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "hello",
			Via: ViaToPigeon,
		},
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if !strings.HasPrefix(lines[0], "sent to pigeon by Alice: ") {
		t.Errorf("expected decorated sender in display line, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[via:to-pigeon]") {
		t.Errorf("expected via tag in metadata, got %q", lines[1])
	}
}

func TestFormatConvMeta_SlackDM(t *testing.T) {
	meta := &ConvMeta{Name: "@Magnus", Type: ConvDM, ChannelID: "D08J1DUQ11Q", UserID: "U08H20E757W"}
	got := FormatConvMeta(meta)
	want := "[type:dm] [channel_id:D08J1DUQ11Q] [user_id:U08H20E757W]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatConvMeta_SlackChannel(t *testing.T) {
	meta := &ConvMeta{Name: "#random", Type: ConvChannel, ChannelID: "C06UQPRB5UH"}
	got := FormatConvMeta(meta)
	want := "[type:channel] [channel_id:C06UQPRB5UH]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatConvMeta_SlackGroupDM(t *testing.T) {
	meta := &ConvMeta{Name: "@mpdm-alice--bob-1", Type: ConvGroupDM, ChannelID: "G01ABC"}
	got := FormatConvMeta(meta)
	want := "[type:group_dm] [channel_id:G01ABC]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatConvMeta_WhatsAppDM(t *testing.T) {
	meta := &ConvMeta{Name: "Alice", Type: ConvDM, JID: "14155551234@s.whatsapp.net", LID: "abc123@lid"}
	got := FormatConvMeta(meta)
	want := "[type:dm] [jid:14155551234@s.whatsapp.net] [lid:abc123@lid]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatConvMeta_WhatsAppGroup(t *testing.T) {
	meta := &ConvMeta{Name: "Family", Type: ConvGroup, JID: "12345@g.us"}
	got := FormatConvMeta(meta)
	want := "[type:group] [jid:12345@g.us]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatConvMeta_Empty(t *testing.T) {
	meta := &ConvMeta{Name: "test"}
	got := FormatConvMeta(meta)
	if got != "" {
		t.Errorf("expected empty string for meta with no IDs or type, got %q", got)
	}
}

func TestFormatMsg_Full(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 2),
			Sender: "Alice", SenderID: "U1", Text: "hello world",
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	if lines[0] != "[2026-03-16 09:15:02] [M1] Alice (U1): hello world" {
		t.Errorf("got %q", lines[0])
	}
}

func TestFormatMsg_WithReactions(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "hello",
		},
		Reactions: []ReactLine{
			{MsgID: "M1", Sender: "Bob", Emoji: "👍"},
			{MsgID: "M1", Sender: "Charlie", Emoji: "👍"},
			{MsgID: "M1", Sender: "Dave", Emoji: "🎉"},
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[1], "👍") || !strings.Contains(lines[1], "🎉") {
		t.Errorf("reactions line = %q", lines[1])
	}
	if !strings.Contains(lines[1], "Bob, Charlie") {
		t.Errorf("expected grouped users, got %q", lines[1])
	}
}

func TestFormatMsg_Reply(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "R1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true,
		},
	}
	lines := FormatMsg(m, time.UTC)
	if !strings.HasPrefix(lines[0], "  ") {
		t.Errorf("reply should be indented, got %q", lines[0])
	}
}

func TestFormatMsg_ReplyWithReactions(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "R1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true,
		},
		Reactions: []ReactLine{
			{MsgID: "R1", Sender: "Alice", Emoji: "👍"},
		},
	}
	lines := FormatMsg(m, time.UTC)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// Reaction line should also be indented
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("reply reaction should be indented, got %q", lines[1])
	}
}

func TestFormatMsg_Timezone(t *testing.T) {
	loc := time.FixedZone("IST", 5*60*60+30*60)
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), // 9:00 UTC
			Sender: "Alice", SenderID: "U1", Text: "hello",
		},
	}
	lines := FormatMsg(m, loc)
	// 9:00 UTC = 14:30 IST
	if !strings.Contains(lines[0], "14:30:00") {
		t.Errorf("expected IST time, got %q", lines[0])
	}
}

func TestNotificationFormat_Basic(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 2),
			Sender: "Alice", SenderID: "U1", Text: "hello world",
		},
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0] != "Alice: hello world" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "  [09:15:02] [message_id:M1] [sender_id:U1]" {
		t.Errorf("line 1 = %q", lines[1])
	}
}

func TestNotificationFormat_Via(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "hello",
			Via: ViaToPigeon,
		},
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if !strings.HasPrefix(lines[0], "sent to pigeon by Alice: ") {
		t.Errorf("expected decorated sender, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[via:to-pigeon]") {
		t.Errorf("expected via tag, got %q", lines[1])
	}
}

func TestNotificationFormat_ReplyTo(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M2", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "yes",
			ReplyTo: "M1",
		},
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if !strings.Contains(lines[1], "[reply_to:M1]") {
		t.Errorf("expected reply_to tag, got %q", lines[1])
	}
}

func TestNotificationFormat_AllOptional(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M2", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Bob", SenderID: "U2", Text: "yes",
			Via: ViaPigeonAsUser, ReplyTo: "M1",
		},
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if !strings.HasPrefix(lines[0], "Bob (via pigeon): ") {
		t.Errorf("expected decorated sender, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "[via:pigeon-as-user]") || !strings.Contains(lines[1], "[reply_to:M1]") {
		t.Errorf("expected both optional tags, got %q", lines[1])
	}
}

func TestNotificationFormat_WithConvMeta(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Magnus", SenderID: "U08H", Text: "hey",
		},
	}
	meta := &ConvMeta{Type: ConvDM, ChannelID: "D08J", UserID: "U08H"}
	lines := formatMsgNotification(m, time.UTC, meta)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	want := "  [09:00:00] [message_id:M1] [sender_id:U08H] [type:dm] [channel_id:D08J] [user_id:U08H]"
	if lines[1] != want {
		t.Errorf("got  %q\nwant %q", lines[1], want)
	}
}

func TestNotificationFormat_WithChannelMeta(t *testing.T) {
	m := ResolvedMsg{
		MsgLine: MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "hello",
		},
	}
	meta := &ConvMeta{Type: ConvChannel, ChannelID: "C06U"}
	lines := formatMsgNotification(m, time.UTC, meta)
	want := "  [09:00:00] [message_id:M1] [sender_id:U1] [type:channel] [channel_id:C06U]"
	if lines[1] != want {
		t.Errorf("got  %q\nwant %q", lines[1], want)
	}
}

func TestFormatDateFileNotification(t *testing.T) {
	f := &ResolvedDateFile{
		Messages: []ResolvedMsg{
			{MsgLine: MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}},
			{MsgLine: MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"}},
		},
	}
	lines := FormatDateFileNotification(f, time.UTC, nil)
	// 2 messages × 2 lines each = 4
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4", len(lines))
	}
	if lines[0] != "Alice: hello" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[2] != "Bob: world" {
		t.Errorf("line 2 = %q", lines[2])
	}
}

func TestFormatDateFileNotification_Nil(t *testing.T) {
	lines := FormatDateFileNotification(nil, time.UTC, nil)
	if lines != nil {
		t.Errorf("expected nil, got %v", lines)
	}
}

func TestFormatDateFileNotification_WithError(t *testing.T) {
	f := &ResolvedDateFile{
		Messages: []ResolvedMsg{
			{MsgLine: MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}},
		},
	}
	lines := FormatDateFileNotification(f, time.UTC, nil, errors.New("read thread 123: file corrupted"))
	// 1 message × 2 lines + 1 warning = 3
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if !strings.HasPrefix(lines[2], "⚠ ") {
		t.Errorf("expected warning prefix, got %q", lines[2])
	}
}

func TestFormatDateFile(t *testing.T) {
	f := &ResolvedDateFile{
		Messages: []ResolvedMsg{
			{
				MsgLine: MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
				Reactions: []ReactLine{
					{MsgID: "M1", Sender: "Bob", Emoji: "👍"},
				},
			},
			{
				MsgLine: MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
			},
		},
	}
	lines := FormatDateFile(f, time.UTC)
	// M1 message + M1 reactions + M2 message = 3 lines
	if len(lines) != 3 {
		t.Errorf("lines = %d, want 3", len(lines))
	}
}

func TestFormatDateFile_Empty(t *testing.T) {
	lines := FormatDateFile(&ResolvedDateFile{}, time.UTC)
	if len(lines) != 0 {
		t.Errorf("lines = %d, want 0", len(lines))
	}
}

func TestFormatDateFile_Nil(t *testing.T) {
	lines := FormatDateFile(nil, time.UTC)
	if lines != nil {
		t.Errorf("expected nil, got %v", lines)
	}
}

func TestFormatDateFile_WithError(t *testing.T) {
	f := &ResolvedDateFile{
		Messages: []ResolvedMsg{
			{MsgLine: MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}},
		},
	}
	lines := FormatDateFile(f, time.UTC, errors.New("read thread 123: file corrupted"))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.HasPrefix(lines[1], "\u26a0 ") {
		t.Errorf("expected warning prefix, got %q", lines[1])
	}
	if !strings.Contains(lines[1], "file corrupted") {
		t.Errorf("expected error text in warning, got %q", lines[1])
	}
}

func TestFormatDateFile_NilErrorNoWarning(t *testing.T) {
	f := &ResolvedDateFile{
		Messages: []ResolvedMsg{
			{MsgLine: MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}},
		},
	}
	lines := FormatDateFile(f, time.UTC, nil)
	if len(lines) != 1 {
		t.Errorf("lines = %d, want 1 (nil error should not add warning)", len(lines))
	}
}

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
		t.Errorf("expected fallback text with link label, got %q", lines[1])
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
			Sender: "ally", SenderID: "U85", Text: "check this screenshot",
			Raw: map[string]any{
				"files": []any{
					map[string]any{
						"name":      "screenshot.png",
						"mimetype":  "image/png",
						"size":      float64(197770),
						"permalink": "https://tubular.slack.com/files/U85/F0AE/screenshot.png",
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
			Sender: "ally", SenderID: "U85", Text: "here",
			Raw: map[string]any{
				"files": []any{
					map[string]any{
						"name":      "doc.pdf",
						"mimetype":  "application/pdf",
						"size":      float64(1048576),
						"permalink": "https://slack.com/files/doc.pdf",
					},
				},
			},
		},
	}
	lines := FormatMsg(m, time.UTC)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "https://slack.com/files/doc.pdf") {
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
			Sender: "Andrew", SenderID: "U036", Text: "see below",
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
	// Message line + file line = 2
	if len(lines) < 2 {
		t.Fatalf("lines = %d, want >= 2", len(lines))
	}
	// Both the message and the file line should be indented for replies
	for _, l := range lines {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("reply line should be indented, got %q", l)
		}
	}
}

