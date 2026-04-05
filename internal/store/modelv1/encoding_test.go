package modelv1

import (
	"strings"
	"testing"
	"time"
)

// ts is a helper that builds a UTC time for tests.
func ts(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

// --- Escaping ---

func TestEscapeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no special chars", "hello world", "hello world"},
		{"single newline", "line1\nline2", `line1\nline2`},
		{"multiple newlines", "a\nb\nc", `a\nb\nc`},
		{"single backslash", `hello\world`, `hello\\world`},
		{"backslash then newline", "hello\\\nworld", `hello\\\nworld`},
		{"newline then backslash", "hello\n\\world", `hello\n\\world`},
		{"only newline", "\n", `\n`},
		{"only backslash", `\`, `\\`},
		{"consecutive backslashes", `\\`, `\\\\`},
		{"consecutive newlines", "\n\n", `\n\n`},
		{"mixed content", "first\nsecond\\third\nfourth", `first\nsecond\\third\nfourth`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeText(tt.in)
			if got != tt.want {
				t.Errorf("EscapeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnescapeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no escapes", "hello world", "hello world"},
		{"escaped newline", `line1\nline2`, "line1\nline2"},
		{"escaped backslash", `hello\\world`, `hello\world`},
		{"escaped backslash then newline", `hello\\\nworld`, "hello\\\nworld"},
		{"trailing backslash", `hello\`, `hello\`},
		{"unknown escape sequence", `hello\tworld`, `hello\tworld`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnescapeText(tt.in)
			if got != tt.want {
				t.Errorf("UnescapeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"plain text",
		"line1\nline2\nline3",
		`backslash\here`,
		"mixed\n\\combo",
		"\n\n\n",
		`\\\`,
		"newline at end\n",
		"\nnewline at start",
	}
	for _, orig := range cases {
		escaped := EscapeText(orig)
		got := UnescapeText(escaped)
		if got != orig {
			t.Errorf("roundtrip(%q): escaped=%q, unescaped=%q", orig, escaped, got)
		}
	}
}

// --- SanitizeSender ---

func TestSanitizeSender(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Alice", "Alice"},
		{"Dr. Smith: Cardiologist", "Dr. Smith Cardiologist"},
		{"No:Colons:Here", "NoColonsHere"},
		{"", ""},
		{":::", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := SanitizeSender(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeSender(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- Marshal + ParseLine round-trips ---

func TestMarshalParseMsg_Basic(t *testing.T) {
	m := MsgLine{
		ID:       "1711568938.123456",
		Ts:       ts(2026, 3, 16, 9, 15, 2),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "hello world",
	}

	line := MarshalMsg(m)
	wantLine := "[2026-03-16 09:15:02 +00:00] [id:1711568938.123456] [from:U04ABCD] Alice: hello world"
	if line != wantLine {
		t.Fatalf("MarshalMsg:\n got  %q\n want %q", line, wantLine)
	}

	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineMessage {
		t.Fatalf("line type = %v, want LineMessage", lt)
	}
	got := v.(MsgLine)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_WithVia(t *testing.T) {
	m := MsgLine{
		ID:       "1711568942.111111",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "User",
		SenderID: "U04USER",
		Via:      ViaPigeonAsUser,
		Text:     "looks great Bob!",
	}

	line := MarshalMsg(m)
	if got := mustParseMsg(t, line); !msgEqual(got, m) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, m)
	}

	// Verify the [via:...] tag is present in the line.
	if want := "[via:pigeon-as-user]"; !containsSubstr(line, want) {
		t.Errorf("line missing %s: %s", want, line)
	}
}

func TestMarshalParseMsg_WithAttachments(t *testing.T) {
	m := MsgLine{
		ID:       "MSG1",
		Ts:       ts(2026, 3, 16, 9, 15, 30),
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Text:     "check this out",
		Attachments: []Attachment{
			{ID: "F07T3", Type: "image/jpeg"},
		},
	}

	line := MarshalMsg(m)
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_MultipleAttachments(t *testing.T) {
	m := MsgLine{
		ID:       "MSG2",
		Ts:       ts(2026, 3, 16, 10, 0, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "two photos",
		Attachments: []Attachment{
			{ID: "F1", Type: "image/jpeg"},
			{ID: "F2", Type: "image/png"},
		},
	}

	line := MarshalMsg(m)
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_EmptyText(t *testing.T) {
	m := MsgLine{
		ID:       "MSG3",
		Ts:       ts(2026, 3, 16, 10, 0, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Attachments: []Attachment{
			{ID: "F07T3", Type: "image/jpeg"},
		},
	}

	line := MarshalMsg(m)
	// Should end with "Alice:" and no trailing space.
	if want := "Alice:"; line[len(line)-len(want):] != want {
		t.Errorf("expected line to end with %q, got: %s", want, line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_ThreadReply(t *testing.T) {
	m := MsgLine{
		ID:       "1711568960.345678",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Reply:    true,
		Text:     "replying here",
	}

	line := MarshalMsg(m)
	// Should start with 2-space indent.
	if line[:2] != "  " {
		t.Fatalf("thread reply missing indent: %q", line)
	}

	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_ReplyTo(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_REPLY",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		ReplyTo:  "QUOTED_MSG_123",
		Text:     "reply text",
	}

	line := MarshalMsg(m)
	if want := "[reply:QUOTED_MSG_123]"; !containsSubstr(line, want) {
		t.Errorf("line missing %s: %s", want, line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_TextWithNewlines(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_NL",
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "line one\nline two\nline three",
	}

	line := MarshalMsg(m)
	// The marshalled line must not contain actual newlines.
	if containsSubstr(line, "\n") {
		t.Fatalf("marshalled line contains newline: %q", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_TextWithBackslashes(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_BS",
		Ts:       ts(2026, 3, 16, 9, 21, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     `path\to\file`,
	}

	line := MarshalMsg(m)
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_TextWithBackslashAndNewline(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_BSNL",
		Ts:       ts(2026, 3, 16, 9, 22, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "before\\\nafter",
	}

	line := MarshalMsg(m)
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMarshalParseMsg_SenderWithColons(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_COLON",
		Ts:       ts(2026, 3, 16, 9, 23, 0),
		Sender:   "Dr. Smith: Cardiologist",
		SenderID: "U04DOC",
		Text:     "hello",
	}

	line := MarshalMsg(m)
	// Sender name should have colons stripped.
	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineMessage {
		t.Fatalf("line type = %v, want LineMessage", lt)
	}
	got := v.(MsgLine)
	// The sender is lossy (colons stripped), so compare against sanitized version.
	if got.Sender != "Dr. Smith Cardiologist" {
		t.Errorf("sender = %q, want %q", got.Sender, "Dr. Smith Cardiologist")
	}
	if got.Text != "hello" {
		t.Errorf("text = %q, want %q", got.Text, "hello")
	}
}

func TestMarshalParseMsg_AllFields(t *testing.T) {
	m := MsgLine{
		ID:       "3EB0A1B2C3D4E5F6",
		Ts:       ts(2026, 4, 1, 14, 30, 45),
		Sender:   "Charlie",
		SenderID: "14155551234@s.whatsapp.net",
		Via:      ViaToPigeon,
		ReplyTo:  "EARLIER_MSG_ID",
		Text:     "replying with\nnewlines and \\backslashes",
		Reply:    true,
		Attachments: []Attachment{
			{ID: "ATTACH1", Type: "application/pdf"},
		},
	}

	line := MarshalMsg(m)
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

// --- Reactions ---

func TestMarshalParseReact(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 16, 30),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
	}

	line := MarshalReact(r)
	wantLine := "[2026-03-16 09:16:30 +00:00] [react:1711568938.123456] [from:U04EFGH] Bob: thumbsup"
	if line != wantLine {
		t.Fatalf("MarshalReact:\n got  %q\n want %q", line, wantLine)
	}

	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineReaction {
		t.Fatalf("line type = %v, want LineReaction", lt)
	}
	got := v.(ReactLine)
	assertReactEqual(t, got, r)
}

func TestMarshalParseReact_Unicode(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Emoji:    "\U0001f44d", // thumbs up emoji
	}

	line := MarshalReact(r)
	got := mustParseReact(t, line)
	assertReactEqual(t, got, r)
}

func TestMarshalParseReact_WithVia(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		MsgID:    "MSG1",
		Sender:   "Bot",
		SenderID: "U04BOT",
		Via:      ViaPigeonAsBot,
		Emoji:    "check",
	}

	line := MarshalReact(r)
	got := mustParseReact(t, line)
	assertReactEqual(t, got, r)
}

func TestMarshalParseUnreact(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 17, 0),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
		Remove:   true,
	}

	line := MarshalUnreact(r)
	if want := "[unreact:"; !containsSubstr(line, want) {
		t.Fatalf("line missing %s: %s", want, line)
	}

	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineUnreaction {
		t.Fatalf("line type = %v, want LineUnreaction", lt)
	}
	got := v.(ReactLine)
	assertReactEqual(t, got, r)
}

// --- Edits ---

func TestMarshalParseEdit(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "updated message text",
	}

	line := MarshalEdit(e)
	wantLine := "[2026-03-16 09:18:00 +00:00] [edit:MSG1] [from:U04ABCD] Alice: updated message text"
	if line != wantLine {
		t.Fatalf("MarshalEdit:\n got  %q\n want %q", line, wantLine)
	}

	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineEdit {
		t.Fatalf("line type = %v, want LineEdit", lt)
	}
	got := v.(EditLine)
	assertEditEqual(t, got, e)
}

func TestMarshalParseEdit_WithNewlines(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "line one\nline two",
	}

	line := MarshalEdit(e)
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

func TestMarshalParseEdit_WithVia(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "User",
		SenderID: "U04USER",
		Via:      ViaPigeonAsUser,
		Text:     "corrected",
	}

	line := MarshalEdit(e)
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

// --- Deletes ---

func TestMarshalParseDelete(t *testing.T) {
	d := DeleteLine{
		Ts:       ts(2026, 3, 16, 9, 19, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
	}

	line := MarshalDelete(d)
	wantLine := "[2026-03-16 09:19:00 +00:00] [delete:MSG1] [from:U04ABCD] Alice:"
	if line != wantLine {
		t.Fatalf("MarshalDelete:\n got  %q\n want %q", line, wantLine)
	}

	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineDelete {
		t.Fatalf("line type = %v, want LineDelete", lt)
	}
	got := v.(DeleteLine)
	assertDeleteEqual(t, got, d)
}

func TestMarshalParseDelete_WithVia(t *testing.T) {
	d := DeleteLine{
		Ts:       ts(2026, 3, 16, 9, 19, 0),
		MsgID:    "MSG1",
		Sender:   "Bot",
		SenderID: "U04BOT",
		Via:      ViaPigeonAsBot,
	}

	line := MarshalDelete(d)
	got := mustParseDelete(t, line)
	assertDeleteEqual(t, got, d)
}

// --- Separator ---

func TestParseSeparator(t *testing.T) {
	lt, v, err := ParseLine("--- channel context ---")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	if lt != LineSeparator {
		t.Fatalf("line type = %v, want LineSeparator", lt)
	}
	if v != nil {
		t.Fatalf("value = %v, want nil", v)
	}
}

// --- ParseLine error cases ---

func TestParseLine_Errors(t *testing.T) {
	bad := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"no bracket", "hello world"},
		{"no tags", "[2026-03-16 09:15:02 +00:00] Alice: hello"},
		{"bad timestamp", "[not-a-timestamp] [id:x] [from:y] Alice: hello"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseLine(tt.line)
			if err == nil {
				t.Errorf("expected error for %q", tt.line)
			}
		})
	}
}

// --- Tag ordering: parser should handle any tag order ---

func TestParseMsg_TagOrderVariations(t *testing.T) {
	// Tags in different orders should all parse to the same result.
	lines := []string{
		"[2026-03-16 09:15:00 +00:00] [id:M1] [from:U1] [via:to-pigeon] [attach:F1 type=image/jpeg] [reply:Q1] Alice: text",
		"[2026-03-16 09:15:00 +00:00] [from:U1] [id:M1] [reply:Q1] [via:to-pigeon] [attach:F1 type=image/jpeg] Alice: text",
		"[2026-03-16 09:15:00 +00:00] [attach:F1 type=image/jpeg] [via:to-pigeon] [reply:Q1] [from:U1] [id:M1] Alice: text",
	}

	for i, line := range lines {
		lt, v, err := ParseLine(line)
		if err != nil {
			t.Fatalf("line[%d]: %v", i, err)
		}
		if lt != LineMessage {
			t.Fatalf("line[%d]: type = %v, want LineMessage", i, lt)
		}
		m := v.(MsgLine)
		if m.ID != "M1" || m.SenderID != "U1" || m.Via != ViaToPigeon ||
			m.ReplyTo != "Q1" || m.Sender != "Alice" || m.Text != "text" ||
			len(m.Attachments) != 1 || m.Attachments[0].ID != "F1" {
			t.Errorf("line[%d]: unexpected parse result: %+v", i, m)
		}
	}
}

// --- Timestamp UTC normalization ---

func TestMarshalMsg_TimestampUTC(t *testing.T) {
	// Provide a non-UTC time; marshal should convert to UTC.
	loc := time.FixedZone("EST", -5*60*60)
	m := MsgLine{
		ID:       "MSG_TZ",
		Ts:       time.Date(2026, 3, 16, 4, 15, 2, 0, loc), // 04:15 EST = 09:15 UTC
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "hello",
	}

	line := MarshalMsg(m)
	if want := "[2026-03-16 09:15:02 +00:00]"; !containsSubstr(line, want) {
		t.Errorf("timestamp not UTC: %s", line)
	}
}

// --- Via enum coverage ---

func TestAllViaValues(t *testing.T) {
	vias := []Via{ViaOrganic, ViaToPigeon, ViaPigeonAsUser, ViaPigeonAsBot}
	for _, via := range vias {
		m := MsgLine{
			ID:       "V",
			Ts:       ts(2026, 1, 1, 0, 0, 0),
			Sender:   "X",
			SenderID: "U",
			Via:      via,
			Text:     "t",
		}
		line := MarshalMsg(m)
		got := mustParseMsg(t, line)
		if got.Via != via {
			t.Errorf("via = %q, want %q", got.Via, via)
		}
	}
}

// --- Full protocol example from spec ---

func TestProtocolExample_DateFile(t *testing.T) {
	// Parse lines from the protocol doc's date file example.
	lines := []string{
		"[2026-03-16 09:15:02 +00:00] [id:1711568938.123456] [from:U04ABCD] Alice: hello world",
		"[2026-03-16 09:15:30 +00:00] [id:1711568940.789012] [from:U04EFGH] [attach:F07T3 type=image/jpeg] Bob: check this out",
		"[2026-03-16 09:16:00 +00:00] [id:1711568942.111111] [from:U04USER] [via:pigeon-as-user] User: looks great Bob!",
		"[2026-03-16 09:16:30 +00:00] [react:1711568938.123456] [from:U04EFGH] Bob: \U0001f44d",
		"[2026-03-16 09:17:00 +00:00] [id:1711568944.222222] [from:U04ALICE] [via:to-pigeon] Alice: hey pigeon, summarize this channel",
		"[2026-03-16 09:17:15 +00:00] [id:1711568945.333333] [from:U04BOT] [via:pigeon-as-bot] pigeon: sure, working on it",
	}

	expected := []LineType{
		LineMessage, LineMessage, LineMessage, LineReaction, LineMessage, LineMessage,
	}

	for i, line := range lines {
		lt, _, err := ParseLine(line)
		if err != nil {
			t.Errorf("line[%d]: %v", i, err)
			continue
		}
		if lt != expected[i] {
			t.Errorf("line[%d]: type = %v, want %v", i, lt, expected[i])
		}
	}
}

func TestProtocolExample_ThreadFile(t *testing.T) {
	lines := []string{
		"[2026-03-16 09:15:30 +00:00] [id:1711568940.789012] [from:U04EFGH] Bob: starting a thread",
		"  [2026-03-16 09:16:00 +00:00] [id:1711568960.345678] [from:U04ABCD] Alice: replying here",
		"  [2026-03-16 09:17:00 +00:00] [id:1711568980.456789] [from:U04EFGH] Bob: thanks",
		"--- channel context ---",
		"[2026-03-16 09:13:00 +00:00] [id:1711568800.111111] [from:U04XYZW] Charlie: context before",
	}

	expectedTypes := []LineType{LineMessage, LineMessage, LineMessage, LineSeparator, LineMessage}
	expectedReply := []bool{false, true, true, false, false}

	for i, line := range lines {
		lt, v, err := ParseLine(line)
		if err != nil {
			t.Errorf("line[%d]: %v", i, err)
			continue
		}
		if lt != expectedTypes[i] {
			t.Errorf("line[%d]: type = %v, want %v", i, lt, expectedTypes[i])
		}
		if lt == LineMessage {
			m := v.(MsgLine)
			if m.Reply != expectedReply[i] {
				t.Errorf("line[%d]: reply = %v, want %v", i, m.Reply, expectedReply[i])
			}
		}
	}
}

// --- helpers ---

func mustParseMsg(t *testing.T, line string) MsgLine {
	t.Helper()
	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", line, err)
	}
	if lt != LineMessage {
		t.Fatalf("ParseLine(%q): type = %v, want LineMessage", line, lt)
	}
	return v.(MsgLine)
}

func mustParseReact(t *testing.T, line string) ReactLine {
	t.Helper()
	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", line, err)
	}
	if lt != LineReaction {
		t.Fatalf("ParseLine(%q): type = %v, want LineReaction", line, lt)
	}
	return v.(ReactLine)
}

func mustParseEdit(t *testing.T, line string) EditLine {
	t.Helper()
	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", line, err)
	}
	if lt != LineEdit {
		t.Fatalf("ParseLine(%q): type = %v, want LineEdit", line, lt)
	}
	return v.(EditLine)
}

func mustParseDelete(t *testing.T, line string) DeleteLine {
	t.Helper()
	lt, v, err := ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", line, err)
	}
	if lt != LineDelete {
		t.Fatalf("ParseLine(%q): type = %v, want LineDelete", line, lt)
	}
	return v.(DeleteLine)
}

func assertMsgEqual(t *testing.T, got, want MsgLine) {
	t.Helper()
	if !msgEqual(got, want) {
		t.Errorf("MsgLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func msgEqual(a, b MsgLine) bool {
	if a.ID != b.ID || !a.Ts.Equal(b.Ts) || a.SenderID != b.SenderID ||
		a.Via != b.Via || a.ReplyTo != b.ReplyTo || a.Reply != b.Reply {
		return false
	}
	// Sender comparison: uses sanitized version of want since colons are stripped.
	if a.Sender != SanitizeSender(b.Sender) {
		return false
	}
	if a.Text != b.Text {
		return false
	}
	if len(a.Attachments) != len(b.Attachments) {
		return false
	}
	for i := range a.Attachments {
		if a.Attachments[i] != b.Attachments[i] {
			return false
		}
	}
	return true
}

func assertReactEqual(t *testing.T, got, want ReactLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != SanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Emoji != want.Emoji || got.Remove != want.Remove {
		t.Errorf("ReactLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func assertEditEqual(t *testing.T, got, want EditLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != SanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Text != want.Text {
		t.Errorf("EditLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func assertDeleteEqual(t *testing.T, got, want DeleteLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != SanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via {
		t.Errorf("DeleteLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func containsSubstr(s, sub string) bool {
	return strings.Contains(s, sub)
}
