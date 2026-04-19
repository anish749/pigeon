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
	m := MsgLine{
		ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Alice", SenderID: "U1", Text: "hello",
		Via: ViaToPigeon,
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
	m := MsgLine{
		ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 2),
		Sender: "Alice", SenderID: "U1", Text: "hello world",
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
	m := MsgLine{
		ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Alice", SenderID: "U1", Text: "hello",
		Via: ViaToPigeon,
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
	m := MsgLine{
		ID: "M2", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Bob", SenderID: "U2", Text: "yes",
		ReplyTo: "M1",
	}
	lines := formatMsgNotification(m, time.UTC, nil)
	if !strings.Contains(lines[1], "[reply_to:M1]") {
		t.Errorf("expected reply_to tag, got %q", lines[1])
	}
}

func TestNotificationFormat_AllOptional(t *testing.T) {
	m := MsgLine{
		ID: "M2", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Bob", SenderID: "U2", Text: "yes",
		Via: ViaPigeonAsUser, ReplyTo: "M1",
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
	m := MsgLine{
		ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Eve", SenderID: "U08H", Text: "hey",
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
	m := MsgLine{
		ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
		Sender: "Alice", SenderID: "U1", Text: "hello",
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

func TestFormatReactionNotification(t *testing.T) {
	msg := MsgLine{
		ID: "M1", Ts: ts(2026, 4, 19, 10, 15, 2),
		Sender: "Bob", SenderID: "U002", Text: "sounds good",
	}
	react := ReactLine{
		Ts: ts(2026, 4, 19, 10, 20, 0), MsgID: "M1",
		Sender: "Alice", SenderID: "U001", Emoji: "thumbsup",
	}

	lines := FormatReactionNotification(msg, react, time.UTC)
	if len(lines) < 2 {
		t.Fatalf("got %d lines, want at least 2", len(lines))
	}
	if !strings.Contains(lines[0], "Alice") || !strings.Contains(lines[0], ":thumbsup:") || !strings.Contains(lines[0], "Bob") {
		t.Errorf("header = %q, want Alice reacted to Bob's message", lines[0])
	}
	if !strings.Contains(lines[0], "sounds good") {
		t.Errorf("header = %q, want message text included", lines[0])
	}
	meta := lines[len(lines)-1]
	if !strings.Contains(meta, "[reaction]") || !strings.Contains(meta, "[message_id:M1]") {
		t.Errorf("meta = %q, want [reaction] and [message_id:M1]", meta)
	}
}

func TestFormatReactionNotification_Remove(t *testing.T) {
	msg := MsgLine{
		ID: "M1", Ts: ts(2026, 4, 19, 10, 15, 2),
		Sender: "Bob", SenderID: "U002", Text: "hello",
	}
	react := ReactLine{
		Ts: ts(2026, 4, 19, 10, 20, 0), MsgID: "M1",
		Sender: "Alice", SenderID: "U001", Emoji: "thumbsup", Remove: true,
	}

	lines := FormatReactionNotification(msg, react, time.UTC)
	if !strings.Contains(lines[0], "removed reaction") {
		t.Errorf("header = %q, want 'removed reaction'", lines[0])
	}
}

func TestFormatReactionNotification_WithRaw(t *testing.T) {
	msg := MsgLine{
		ID: "M1", Ts: ts(2026, 4, 19, 10, 15, 2),
		Sender: "Bob", SenderID: "U002", Text: "hello",
		Raw: map[string]any{"blocks": []any{"test"}},
	}
	react := ReactLine{
		Ts: ts(2026, 4, 19, 10, 20, 0), MsgID: "M1",
		Sender: "Alice", SenderID: "U001", Emoji: "eyes",
	}

	lines := FormatReactionNotification(msg, react, time.UTC)
	if len(lines) < 3 {
		t.Fatalf("got %d lines, want at least 3 (header + raw + meta)", len(lines))
	}
	if !strings.Contains(lines[1], "blocks") {
		t.Errorf("raw line = %q, want blocks JSON", lines[1])
	}
}

func TestFormatReactionFallbackNotification(t *testing.T) {
	react := ReactLine{
		Ts: ts(2026, 4, 19, 10, 20, 0), MsgID: "M1",
		Sender: "Alice", SenderID: "U001", Emoji: "thumbsup",
	}

	lines := FormatReactionFallbackNotification(react, time.UTC)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "Alice") || !strings.Contains(lines[0], ":thumbsup:") {
		t.Errorf("header = %q, want Alice and emoji", lines[0])
	}
	if !strings.Contains(lines[0], "reacted with") {
		t.Errorf("header = %q, want 'reacted with'", lines[0])
	}
	if !strings.Contains(lines[1], "[reaction]") || !strings.Contains(lines[1], "[message_id:M1]") {
		t.Errorf("meta = %q, want [reaction] and [message_id:M1]", lines[1])
	}
}

func TestFormatReactionFallbackNotification_Remove(t *testing.T) {
	react := ReactLine{
		Ts: ts(2026, 4, 19, 10, 20, 0), MsgID: "M1",
		Sender: "Alice", SenderID: "U001", Emoji: "thumbsup", Remove: true,
	}

	lines := FormatReactionFallbackNotification(react, time.UTC)
	if !strings.Contains(lines[0], "removed reaction") {
		t.Errorf("header = %q, want 'removed reaction'", lines[0])
	}
}
