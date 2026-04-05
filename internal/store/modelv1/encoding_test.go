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

// --- Escaping round-trip ---

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
		escaped := escapeText(orig)
		got := unescapeText(escaped)
		if got != orig {
			t.Errorf("roundtrip(%q): escaped=%q, unescaped=%q", orig, escaped, got)
		}
	}
}

// --- Marshal + Parse round-trips ---

func TestMsg_Basic(t *testing.T) {
	m := MsgLine{
		ID:       "1711568938.123456",
		Ts:       ts(2026, 3, 16, 9, 15, 2),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "hello world",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	want := "[2026-03-16 09:15:02 +00:00] [id:1711568938.123456] [from:U04ABCD] Alice: hello world"
	if line != want {
		t.Fatalf("Marshal:\n got  %q\n want %q", line, want)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_WithVia(t *testing.T) {
	m := MsgLine{
		ID:       "1711568942.111111",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "User",
		SenderID: "U04USER",
		Via:      ViaPigeonAsUser,
		Text:     "looks great Bob!",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if !strings.Contains(line, "[via:pigeon-as-user]") {
		t.Errorf("line missing [via:pigeon-as-user]: %s", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_WithAttachments(t *testing.T) {
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
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_MultipleAttachments(t *testing.T) {
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
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_EmptyText(t *testing.T) {
	m := MsgLine{
		ID:       "MSG3",
		Ts:       ts(2026, 3, 16, 10, 0, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Attachments: []Attachment{
			{ID: "F07T3", Type: "image/jpeg"},
		},
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if !strings.HasSuffix(line, "Alice:") {
		t.Errorf("expected line to end with 'Alice:', got: %s", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_ThreadReply(t *testing.T) {
	m := MsgLine{
		ID:       "1711568960.345678",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Reply:    true,
		Text:     "replying here",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if line[:2] != "  " {
		t.Fatalf("thread reply missing indent: %q", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_ReplyTo(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_REPLY",
		Ts:       ts(2026, 3, 16, 9, 16, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		ReplyTo:  "QUOTED_MSG_123",
		Text:     "reply text",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if !strings.Contains(line, "[reply:QUOTED_MSG_123]") {
		t.Errorf("line missing [reply:QUOTED_MSG_123]: %s", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_TextWithNewlines(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_NL",
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "line one\nline two\nline three",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if strings.Contains(line, "\n") {
		t.Fatalf("marshalled line contains newline: %q", line)
	}
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_TextWithBackslashes(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_BS",
		Ts:       ts(2026, 3, 16, 9, 21, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     `path\to\file`,
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_TextWithBackslashAndNewline(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_BSNL",
		Ts:       ts(2026, 3, 16, 9, 22, 0),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "before\\\nafter",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

func TestMsg_SenderWithColons(t *testing.T) {
	m := MsgLine{
		ID:       "MSG_COLON",
		Ts:       ts(2026, 3, 16, 9, 23, 0),
		Sender:   "Dr. Smith: Cardiologist",
		SenderID: "U04DOC",
		Text:     "hello",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	if got.Sender != "Dr. Smith Cardiologist" {
		t.Errorf("sender = %q, want %q", got.Sender, "Dr. Smith Cardiologist")
	}
	if got.Text != "hello" {
		t.Errorf("text = %q, want %q", got.Text, "hello")
	}
}

func TestMsg_AllFields(t *testing.T) {
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
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
}

// --- Reactions ---

func TestReact(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 16, 30),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
	}
	line := mustMarshal(t, Line{Type: LineReaction, React: &r})
	want := "[2026-03-16 09:16:30 +00:00] [react:1711568938.123456] [from:U04EFGH] Bob: thumbsup"
	if line != want {
		t.Fatalf("Marshal:\n got  %q\n want %q", line, want)
	}
	got := mustParseReact(t, line)
	assertReactEqual(t, got, r)
}

func TestReact_Unicode(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Emoji:    "\U0001f44d",
	}
	line := mustMarshal(t, Line{Type: LineReaction, React: &r})
	got := mustParseReact(t, line)
	assertReactEqual(t, got, r)
}

func TestReact_WithVia(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 20, 0),
		MsgID:    "MSG1",
		Sender:   "Bot",
		SenderID: "U04BOT",
		Via:      ViaPigeonAsBot,
		Emoji:    "check",
	}
	line := mustMarshal(t, Line{Type: LineReaction, React: &r})
	got := mustParseReact(t, line)
	assertReactEqual(t, got, r)
}

func TestUnreact(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 17, 0),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
		Remove:   true,
	}
	line := mustMarshal(t, Line{Type: LineUnreaction, React: &r})
	if !strings.Contains(line, "[unreact:") {
		t.Fatalf("line missing [unreact:: %s", line)
	}
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != LineUnreaction {
		t.Fatalf("type = %v, want LineUnreaction", parsed.Type)
	}
	assertReactEqual(t, *parsed.React, r)
}

// --- Edits ---

func TestEdit(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "updated message text",
	}
	line := mustMarshal(t, Line{Type: LineEdit, Edit: &e})
	want := "[2026-03-16 09:18:00 +00:00] [edit:MSG1] [from:U04ABCD] Alice: updated message text"
	if line != want {
		t.Fatalf("Marshal:\n got  %q\n want %q", line, want)
	}
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

func TestEdit_WithNewlines(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "line one\nline two",
	}
	line := mustMarshal(t, Line{Type: LineEdit, Edit: &e})
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

func TestEdit_WithVia(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "User",
		SenderID: "U04USER",
		Via:      ViaPigeonAsUser,
		Text:     "corrected",
	}
	line := mustMarshal(t, Line{Type: LineEdit, Edit: &e})
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

func TestEdit_WithAttachments(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "updated caption",
		Attachments: []Attachment{
			{ID: "F1", Type: "image/jpeg"},
			{ID: "F2", Type: "image/png"},
		},
	}
	line := mustMarshal(t, Line{Type: LineEdit, Edit: &e})
	if !strings.Contains(line, "[attach:F1 type=image/jpeg]") {
		t.Errorf("line missing attachment: %s", line)
	}
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

// --- Deletes ---

func TestDelete(t *testing.T) {
	d := DeleteLine{
		Ts:       ts(2026, 3, 16, 9, 19, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
	}
	line := mustMarshal(t, Line{Type: LineDelete, Delete: &d})
	want := "[2026-03-16 09:19:00 +00:00] [delete:MSG1] [from:U04ABCD] Alice:"
	if line != want {
		t.Fatalf("Marshal:\n got  %q\n want %q", line, want)
	}
	got := mustParseDelete(t, line)
	assertDeleteEqual(t, got, d)
}

func TestDelete_WithVia(t *testing.T) {
	d := DeleteLine{
		Ts:       ts(2026, 3, 16, 9, 19, 0),
		MsgID:    "MSG1",
		Sender:   "Bot",
		SenderID: "U04BOT",
		Via:      ViaPigeonAsBot,
	}
	line := mustMarshal(t, Line{Type: LineDelete, Delete: &d})
	got := mustParseDelete(t, line)
	assertDeleteEqual(t, got, d)
}

// --- Separator ---

func TestSeparator(t *testing.T) {
	line := mustMarshal(t, Line{Type: LineSeparator})
	if line != SeparatorLine {
		t.Fatalf("got %q, want %q", line, SeparatorLine)
	}
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != LineSeparator {
		t.Fatalf("type = %v, want LineSeparator", parsed.Type)
	}
}

// --- Parse error cases ---

func TestParse_Errors(t *testing.T) {
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
			_, err := Parse(tt.line)
			if err == nil {
				t.Errorf("expected error for %q", tt.line)
			}
		})
	}
}

// --- Tag ordering ---

func TestParse_TagOrderVariations(t *testing.T) {
	lines := []string{
		"[2026-03-16 09:15:00 +00:00] [id:M1] [from:U1] [via:to-pigeon] [attach:F1 type=image/jpeg] [reply:Q1] Alice: text",
		"[2026-03-16 09:15:00 +00:00] [from:U1] [id:M1] [reply:Q1] [via:to-pigeon] [attach:F1 type=image/jpeg] Alice: text",
		"[2026-03-16 09:15:00 +00:00] [attach:F1 type=image/jpeg] [via:to-pigeon] [reply:Q1] [from:U1] [id:M1] Alice: text",
	}
	for i, line := range lines {
		parsed, err := Parse(line)
		if err != nil {
			t.Fatalf("line[%d]: %v", i, err)
		}
		if parsed.Type != LineMessage {
			t.Fatalf("line[%d]: type = %v, want LineMessage", i, parsed.Type)
		}
		m := parsed.Msg
		if m.ID != "M1" || m.SenderID != "U1" || m.Via != ViaToPigeon ||
			m.ReplyTo != "Q1" || m.Sender != "Alice" || m.Text != "text" ||
			len(m.Attachments) != 1 || m.Attachments[0].ID != "F1" {
			t.Errorf("line[%d]: unexpected result: %+v", i, m)
		}
	}
}

// --- Timestamp UTC normalization ---

func TestMarshal_TimestampUTC(t *testing.T) {
	loc := time.FixedZone("EST", -5*60*60)
	m := MsgLine{
		ID:       "MSG_TZ",
		Ts:       time.Date(2026, 3, 16, 4, 15, 2, 0, loc), // 04:15 EST = 09:15 UTC
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "hello",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	if !strings.Contains(line, "[2026-03-16 09:15:02 +00:00]") {
		t.Errorf("timestamp not UTC: %s", line)
	}
}

// --- Via enum coverage ---

func TestAllViaValues(t *testing.T) {
	for _, via := range []Via{ViaOrganic, ViaToPigeon, ViaPigeonAsUser, ViaPigeonAsBot} {
		m := MsgLine{
			ID: "V", Ts: ts(2026, 1, 1, 0, 0, 0),
			Sender: "X", SenderID: "U", Via: via, Text: "t",
		}
		line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
		got := mustParseMsg(t, line)
		if got.Via != via {
			t.Errorf("via = %q, want %q", got.Via, via)
		}
	}
}

// --- Protocol spec examples ---

func TestProtocolExample_DateFile(t *testing.T) {
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
		parsed, err := Parse(line)
		if err != nil {
			t.Errorf("line[%d]: %v", i, err)
			continue
		}
		if parsed.Type != expected[i] {
			t.Errorf("line[%d]: type = %v, want %v", i, parsed.Type, expected[i])
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
		parsed, err := Parse(line)
		if err != nil {
			t.Errorf("line[%d]: %v", i, err)
			continue
		}
		if parsed.Type != expectedTypes[i] {
			t.Errorf("line[%d]: type = %v, want %v", i, parsed.Type, expectedTypes[i])
		}
		if parsed.Type == LineMessage && parsed.Msg.Reply != expectedReply[i] {
			t.Errorf("line[%d]: reply = %v, want %v", i, parsed.Msg.Reply, expectedReply[i])
		}
	}
}

// --- helpers ---

func mustMarshal(t *testing.T, l Line) string {
	t.Helper()
	s, err := Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return s
}

func mustParseMsg(t *testing.T, line string) MsgLine {
	t.Helper()
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse(%q): %v", line, err)
	}
	if parsed.Type != LineMessage || parsed.Msg == nil {
		t.Fatalf("Parse(%q): type = %v, want LineMessage", line, parsed.Type)
	}
	return *parsed.Msg
}

func mustParseReact(t *testing.T, line string) ReactLine {
	t.Helper()
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse(%q): %v", line, err)
	}
	if parsed.Type != LineReaction || parsed.React == nil {
		t.Fatalf("Parse(%q): type = %v, want LineReaction", line, parsed.Type)
	}
	return *parsed.React
}

func mustParseEdit(t *testing.T, line string) EditLine {
	t.Helper()
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse(%q): %v", line, err)
	}
	if parsed.Type != LineEdit || parsed.Edit == nil {
		t.Fatalf("Parse(%q): type = %v, want LineEdit", line, parsed.Type)
	}
	return *parsed.Edit
}

func mustParseDelete(t *testing.T, line string) DeleteLine {
	t.Helper()
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse(%q): %v", line, err)
	}
	if parsed.Type != LineDelete || parsed.Delete == nil {
		t.Fatalf("Parse(%q): type = %v, want LineDelete", line, parsed.Type)
	}
	return *parsed.Delete
}

func assertMsgEqual(t *testing.T, got, want MsgLine) {
	t.Helper()
	if got.ID != want.ID || !got.Ts.Equal(want.Ts) || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.ReplyTo != want.ReplyTo || got.Reply != want.Reply ||
		got.Sender != sanitizeSender(want.Sender) || got.Text != want.Text {
		t.Errorf("MsgLine mismatch:\n got  %+v\n want %+v", got, want)
	}
	if len(got.Attachments) != len(want.Attachments) {
		t.Errorf("attachments count: got %d, want %d", len(got.Attachments), len(want.Attachments))
		return
	}
	for i := range got.Attachments {
		if got.Attachments[i] != want.Attachments[i] {
			t.Errorf("attachment[%d]: got %+v, want %+v", i, got.Attachments[i], want.Attachments[i])
		}
	}
}

func assertReactEqual(t *testing.T, got, want ReactLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != sanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Emoji != want.Emoji || got.Remove != want.Remove {
		t.Errorf("ReactLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func assertEditEqual(t *testing.T, got, want EditLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != sanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Text != want.Text {
		t.Errorf("EditLine mismatch:\n got  %+v\n want %+v", got, want)
	}
	if len(got.Attachments) != len(want.Attachments) {
		t.Errorf("attachments count: got %d, want %d", len(got.Attachments), len(want.Attachments))
		return
	}
	for i := range got.Attachments {
		if got.Attachments[i] != want.Attachments[i] {
			t.Errorf("attachment[%d]: got %+v, want %+v", i, got.Attachments[i], want.Attachments[i])
		}
	}
}

func assertDeleteEqual(t *testing.T, got, want DeleteLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != sanitizeSender(want.Sender) || got.SenderID != want.SenderID ||
		got.Via != want.Via {
		t.Errorf("DeleteLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}
