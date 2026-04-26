package modelv1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ts is a helper that builds a UTC time for tests.
func ts(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

// --- Marshal/Parse round-trips ---

func TestMsg_RoundTrip(t *testing.T) {
	m := MsgLine{
		ID:       "1711568938.123456",
		Ts:       ts(2026, 3, 16, 9, 15, 2),
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "hello world",
	}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})
	got := mustParseMsg(t, line)
	assertMsgEqual(t, got, m)
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
	// JSON preserves colons — no lossy transformation
	if got.Sender != "Dr. Smith: Cardiologist" {
		t.Errorf("sender = %q, want %q", got.Sender, "Dr. Smith: Cardiologist")
	}
	if got.Text != "hello" {
		t.Errorf("text = %q, want %q", got.Text, "hello")
	}
}

func TestReact_RoundTrip(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 16, 30),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
	}
	line := mustMarshal(t, Line{Type: LineReaction, React: &r})
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

func TestUnreact_RoundTrip(t *testing.T) {
	r := ReactLine{
		Ts:       ts(2026, 3, 16, 9, 17, 0),
		MsgID:    "1711568938.123456",
		Sender:   "Bob",
		SenderID: "U04EFGH",
		Emoji:    "thumbsup",
		Remove:   true,
	}
	line := mustMarshal(t, Line{Type: LineUnreaction, React: &r})
	parsed, err := Parse(line)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Type != LineUnreaction {
		t.Fatalf("type = %v, want LineUnreaction", parsed.Type)
	}
	assertReactEqual(t, *parsed.React, r)
}

func TestEdit_RoundTrip(t *testing.T) {
	e := EditLine{
		Ts:       ts(2026, 3, 16, 9, 18, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
		Text:     "updated message text",
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
	got := mustParseEdit(t, line)
	assertEditEqual(t, got, e)
}

func TestDelete_RoundTrip(t *testing.T) {
	d := DeleteLine{
		Ts:       ts(2026, 3, 16, 9, 19, 0),
		MsgID:    "MSG1",
		Sender:   "Alice",
		SenderID: "U04ABCD",
	}
	line := mustMarshal(t, Line{Type: LineDelete, Delete: &d})
	got := mustParseDelete(t, line)
	assertDeleteEqual(t, got, d)
}

// TestEditDelete_ThreadFields verifies thread_ts and thread_id round-trip
// for EditLine and DeleteLine, and that omitempty keeps both keys off
// disk when the event targets a top-level (non-thread) message.
func TestEditDelete_ThreadFields(t *testing.T) {
	tests := []struct {
		name      string
		threadTS  string
		threadID  string
		wantTSKey bool // true ⇒ "thread_ts" must appear on disk
		wantIDKey bool // true ⇒ "thread_id" must appear on disk
	}{
		{name: "no thread", threadTS: "", threadID: "", wantTSKey: false, wantIDKey: false},
		{name: "slack-style (both set)", threadTS: "1700000001.000001", threadID: "1700000001.000001", wantTSKey: true, wantIDKey: true},
		{name: "thread_id only (whatsapp-style)", threadTS: "", threadID: "WAMSG_PARENT", wantTSKey: false, wantIDKey: true},
	}

	for _, tt := range tests {
		t.Run("EditLine/"+tt.name, func(t *testing.T) {
			e := EditLine{
				Ts: ts(2026, 3, 16, 9, 18, 0), MsgID: "MSG1",
				Sender: "Alice", SenderID: "U1", Text: "edited",
				ThreadTS: tt.threadTS, ThreadID: tt.threadID,
			}
			line := mustMarshal(t, Line{Type: LineEdit, Edit: &e})
			if got := strings.Contains(line, `"thread_ts"`); got != tt.wantTSKey {
				t.Errorf("on-disk thread_ts presence = %v, want %v\nline: %s", got, tt.wantTSKey, line)
			}
			if got := strings.Contains(line, `"thread_id"`); got != tt.wantIDKey {
				t.Errorf("on-disk thread_id presence = %v, want %v\nline: %s", got, tt.wantIDKey, line)
			}
			parsed := mustParseEdit(t, line)
			assertEditEqual(t, parsed, e)
		})

		t.Run("DeleteLine/"+tt.name, func(t *testing.T) {
			d := DeleteLine{
				Ts: ts(2026, 3, 16, 9, 19, 0), MsgID: "MSG1",
				Sender: "Alice", SenderID: "U1",
				ThreadTS: tt.threadTS, ThreadID: tt.threadID,
			}
			line := mustMarshal(t, Line{Type: LineDelete, Delete: &d})
			if got := strings.Contains(line, `"thread_ts"`); got != tt.wantTSKey {
				t.Errorf("on-disk thread_ts presence = %v, want %v\nline: %s", got, tt.wantTSKey, line)
			}
			if got := strings.Contains(line, `"thread_id"`); got != tt.wantIDKey {
				t.Errorf("on-disk thread_id presence = %v, want %v\nline: %s", got, tt.wantIDKey, line)
			}
			parsed := mustParseDelete(t, line)
			assertDeleteEqual(t, parsed, d)
		})
	}
}

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

func TestParse_Errors(t *testing.T) {
	bad := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"not json", "hello world"},
		{"no type", `{"id":"M1"}`},
		{"bad type", `{"type":"bogus","id":"M1"}`},
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

func TestMarshal_TypeFieldPresent(t *testing.T) {
	m := MsgLine{ID: "M1", Ts: ts(2026, 1, 1, 0, 0, 0), Sender: "X", SenderID: "U"}
	line := mustMarshal(t, Line{Type: LineMessage, Msg: &m})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	typ, ok := raw["type"]
	if !ok {
		t.Fatal("missing 'type' field in marshalled JSON")
	}
	if string(typ) != `"msg"` {
		t.Errorf("type = %s, want %q", typ, "msg")
	}
}

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

// --- helpers ---

func mustMarshal(t *testing.T, l Line) string {
	t.Helper()
	data, err := Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(data)
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
		got.Sender != want.Sender || got.Text != want.Text {
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
		got.Sender != want.Sender || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Emoji != want.Emoji || got.Remove != want.Remove {
		t.Errorf("ReactLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func assertEditEqual(t *testing.T, got, want EditLine) {
	t.Helper()
	if got.MsgID != want.MsgID || !got.Ts.Equal(want.Ts) ||
		got.Sender != want.Sender || got.SenderID != want.SenderID ||
		got.Via != want.Via || got.Text != want.Text ||
		got.ThreadTS != want.ThreadTS || got.ThreadID != want.ThreadID {
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
		got.Sender != want.Sender || got.SenderID != want.SenderID ||
		got.Via != want.Via ||
		got.ThreadTS != want.ThreadTS || got.ThreadID != want.ThreadID {
		t.Errorf("DeleteLine mismatch:\n got  %+v\n want %+v", got, want)
	}
}
