package search

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func ts(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

func jsonLine(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func msgJSON(id string, t time.Time, sender, senderID, text string) string {
	m := struct {
		Type     string    `json:"type"`
		ID       string    `json:"id"`
		Ts       time.Time `json:"ts"`
		Sender   string    `json:"sender"`
		SenderID string    `json:"from"`
		Text     string    `json:"text"`
	}{"msg", id, t, sender, senderID, text}
	return jsonLine(m)
}

func reactJSON(t time.Time, msgID, sender, senderID, emoji string) string {
	r := struct {
		Type     string    `json:"type"`
		Ts       time.Time `json:"ts"`
		MsgID    string    `json:"msg"`
		Sender   string    `json:"sender"`
		SenderID string    `json:"from"`
		Emoji    string    `json:"emoji"`
	}{"react", t, msgID, sender, senderID, emoji}
	return jsonLine(r)
}

// rgMatchJSON wraps content in an rg --json "match" envelope.
func rgMatchJSON(path, content string) string {
	envelope := struct {
		Type string `json:"type"`
		Data struct {
			Path  struct{ Text string } `json:"path"`
			Lines struct{ Text string } `json:"lines"`
		} `json:"data"`
	}{}
	envelope.Type = "match"
	envelope.Data.Path.Text = path
	envelope.Data.Lines.Text = content + "\n"
	return jsonLine(envelope)
}

// rgContextJSON wraps content in an rg --json "context" envelope.
func rgContextJSON(path, content string) string {
	envelope := struct {
		Type string `json:"type"`
		Data struct {
			Path  struct{ Text string } `json:"path"`
			Lines struct{ Text string } `json:"lines"`
		} `json:"data"`
	}{}
	envelope.Type = "context"
	envelope.Data.Path.Text = path
	envelope.Data.Lines.Text = content + "\n"
	return jsonLine(envelope)
}

// rgBeginJSON returns an rg --json "begin" line.
func rgBeginJSON(path string) string {
	return fmt.Sprintf(`{"type":"begin","data":{"path":{"text":%q}}}`, path)
}

// --- ParseGrepOutput ---

func TestParseGrepOutput_BasicMessages(t *testing.T) {
	path := "/data/slack/acme-corp/#general/2026-03-16.jsonl"
	lines := []string{
		rgBeginJSON(path),
		rgMatchJSON(path, msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done")),
		rgMatchJSON(path, msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "nice deploy")),
	}
	output := []byte(strings.Join(lines, "\n"))

	matches, _ := ParseGrepOutput(output, "/data")
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	if matches[0].Msg.ID != "M1" {
		t.Errorf("match[0].Msg.ID = %q, want M1", matches[0].Msg.ID)
	}
	if matches[0].Msg.Sender != "Alice" {
		t.Errorf("match[0].Msg.Sender = %q, want Alice", matches[0].Msg.Sender)
	}
	if matches[0].Platform != "slack" {
		t.Errorf("match[0].Platform = %q, want slack", matches[0].Platform)
	}
	if matches[0].Account != "acme-corp" {
		t.Errorf("match[0].Account = %q, want acme-corp", matches[0].Account)
	}
	if matches[0].Conversation != "#general" {
		t.Errorf("match[0].Conversation = %q, want #general", matches[0].Conversation)
	}
	if matches[0].Date != "2026-03-16" {
		t.Errorf("match[0].Date = %q, want 2026-03-16", matches[0].Date)
	}
}

func TestParseGrepOutput_SkipsNonMessageEvents(t *testing.T) {
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	lines := []string{
		rgMatchJSON(path, msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")),
		rgMatchJSON(path, reactJSON(ts(2026, 3, 16, 9, 1, 0), "M1", "Bob", "U2", "thumbsup")),
		rgMatchJSON(path, `{"type":"edit","ts":"2026-03-16T09:02:00Z","msg":"M1","sender":"Alice","from":"U1","text":"updated"}`),
		rgMatchJSON(path, `{"type":"delete","ts":"2026-03-16T09:03:00Z","msg":"M1","sender":"Alice","from":"U1"}`),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches, _ := ParseGrepOutput(output, "/data")
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1 (only msg events)", len(matches))
	}
}

func TestParseGrepOutput_SkipsNonMatchTypes(t *testing.T) {
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	lines := []string{
		rgBeginJSON(path),
		rgMatchJSON(path, msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "first")),
		`{"type":"summary","data":{"stats":{}}}`,
		rgMatchJSON(path, msgJSON("M2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "second")),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches, _ := ParseGrepOutput(output, "/data")
	if len(matches) != 2 {
		t.Errorf("matches = %d, want 2", len(matches))
	}
}

func TestParseGrepOutput_ContextLines(t *testing.T) {
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	lines := []string{
		rgContextJSON(path, msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "context before")),
		rgMatchJSON(path, msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "the match")),
		rgContextJSON(path, msgJSON("M3", ts(2026, 3, 16, 9, 2, 0), "Alice", "U1", "context after")),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches, _ := ParseGrepOutput(output, "/data")
	if len(matches) != 3 {
		t.Errorf("matches = %d, want 3 (match + context lines)", len(matches))
	}
}

func TestParseGrepOutput_TextWithBraces(t *testing.T) {
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	content := `{"type":"msg","id":"M1","ts":"2026-03-16T09:00:00Z","sender":"Alice","from":"U1","text":"meeting at {office} tomorrow"}`
	line := rgMatchJSON(path, content)
	matches, _ := ParseGrepOutput([]byte(line), "/data")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Msg.Text != "meeting at {office} tomorrow" {
		t.Errorf("text = %q, want 'meeting at {office} tomorrow'", matches[0].Msg.Text)
	}
}

func TestParseGrepOutput_TextWithEmbeddedJSON(t *testing.T) {
	// This was the bug — message text containing :{ would break the old delimiter split.
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	content := `{"type":"msg","id":"M1","ts":"2026-03-16T09:00:00Z","sender":"Alice","from":"U1","text":"query:{\"field\":\"value\"}"}`
	line := rgMatchJSON(path, content)
	matches, err := ParseGrepOutput([]byte(line), "/data")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
}

func TestParseGrepOutput_ReturnsErrorForBadJSON(t *testing.T) {
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	lines := []string{
		rgMatchJSON(path, msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "good")),
		rgMatchJSON(path, "{bad json here}"),
		rgMatchJSON(path, msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "also good")),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches, err := ParseGrepOutput(output, "/data")
	if err == nil {
		t.Error("expected error for bad JSON line, got nil")
	}
	if len(matches) != 2 {
		t.Errorf("matches = %d, want 2 (bad line skipped but error returned)", len(matches))
	}
}

func TestParseGrepOutput_EmptyOutput(t *testing.T) {
	matches, _ := ParseGrepOutput(nil, "/data")
	if len(matches) != 0 {
		t.Errorf("matches = %d, want 0", len(matches))
	}
}

func TestParseGrepOutput_MultipleConversations(t *testing.T) {
	lines := []string{
		rgMatchJSON("/data/slack/acme/#general/2026-03-16.jsonl", msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy")),
		rgMatchJSON("/data/slack/acme/#random/2026-03-16.jsonl", msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy too")),
		rgMatchJSON("/data/whatsapp/15551234567/+14155551234/2026-03-16.jsonl", msgJSON("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "C1", "deploy three")),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches, _ := ParseGrepOutput(output, "/data")
	if len(matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(matches))
	}
	if matches[0].Conversation != "#general" {
		t.Errorf("match[0].Conversation = %q, want #general", matches[0].Conversation)
	}
	if matches[1].Conversation != "#random" {
		t.Errorf("match[1].Conversation = %q, want #random", matches[1].Conversation)
	}
	if matches[2].Platform != "whatsapp" {
		t.Errorf("match[2].Platform = %q, want whatsapp", matches[2].Platform)
	}
}

func TestParseGrepOutput_PreservesMessageFields(t *testing.T) {
	msg := struct {
		Type     string               `json:"type"`
		ID       string               `json:"id"`
		Ts       time.Time            `json:"ts"`
		Sender   string               `json:"sender"`
		SenderID string               `json:"from"`
		Via      modelv1.Via          `json:"via"`
		Text     string               `json:"text"`
		ReplyTo  string               `json:"replyTo"`
		Reply    bool                 `json:"reply"`
		Attach   []modelv1.Attachment `json:"attach"`
	}{
		"msg", "M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1",
		modelv1.ViaPigeonAsUser, "hello\nworld", "Q1", true,
		[]modelv1.Attachment{{ID: "F1", Type: "image/jpeg"}},
	}
	path := "/data/slack/acme/ch/2026-03-16.jsonl"
	line := rgMatchJSON(path, jsonLine(msg))
	matches, _ := ParseGrepOutput([]byte(line), "/data")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	m := matches[0].Msg
	if m.Via != modelv1.ViaPigeonAsUser {
		t.Errorf("Via = %q, want pigeon-as-user", m.Via)
	}
	if m.Text != "hello\nworld" {
		t.Errorf("Text = %q, want hello\\nworld", m.Text)
	}
	if m.ReplyTo != "Q1" {
		t.Errorf("ReplyTo = %q, want Q1", m.ReplyTo)
	}
	if !m.Reply {
		t.Error("Reply = false, want true")
	}
	if len(m.Attachments) != 1 || m.Attachments[0].ID != "F1" {
		t.Errorf("Attachments = %+v, want [{F1 image/jpeg}]", m.Attachments)
	}
}

// --- ParseFilePath ---

func TestParseFilePath_FullDepth(t *testing.T) {
	plat, acct, conv, date, _, _ := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.jsonl", "/data")
	if plat != "slack" || acct != "acme-corp" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (slack, acme-corp, #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_PlatformScope(t *testing.T) {
	plat, acct, conv, date, _, _ := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.jsonl", "/data/slack")
	if plat != "" || acct != "acme-corp" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (, acme-corp, #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_AccountScope(t *testing.T) {
	plat, acct, conv, date, _, _ := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.jsonl", "/data/slack/acme-corp")
	if plat != "" || acct != "" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (, , #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_ThreadFile(t *testing.T) {
	plat, acct, conv, date, thread, _ := ParseFilePath("/data/slack/acme-corp/#general/threads/1711568940.789012.jsonl", "/data")
	if plat != "slack" || acct != "acme-corp" || conv != "#general" || date != "1711568940.789012" {
		t.Errorf("got (%q, %q, %q, %q), want (slack, acme-corp, #general, 1711568940.789012)", plat, acct, conv, date)
	}
	if !thread {
		t.Error("thread = false, want true")
	}
}

func TestParseFilePath_ThreadFile_AccountScope(t *testing.T) {
	plat, acct, conv, date, thread, _ := ParseFilePath("/data/slack/acme-corp/#general/threads/1711568940.789012.jsonl", "/data/slack/acme-corp")
	if plat != "" || acct != "" || conv != "#general" || date != "1711568940.789012" {
		t.Errorf("got (%q, %q, %q, %q), want (, , #general, 1711568940.789012)", plat, acct, conv, date)
	}
	if !thread {
		t.Error("thread = false, want true")
	}
}

func TestParseFilePath_DateFile_NotThread(t *testing.T) {
	_, _, _, _, thread, _ := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.jsonl", "/data")
	if thread {
		t.Error("thread = true, want false for date file")
	}
}

// --- FilterThreadsBySince ---

func TestFilterThreadsBySince_KeepsAliveThreads(t *testing.T) {
	now := time.Now()
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			Msg: modelv1.MsgLine{ID: "M1", Ts: now.Add(-1 * time.Hour)}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "1711568940", Thread: true,
			Msg: modelv1.MsgLine{ID: "T1", Ts: now.Add(-30 * time.Minute)}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "1711568940", Thread: true,
			Msg: modelv1.MsgLine{ID: "T2", Ts: now.Add(-48 * time.Hour)}},
		{Platform: "slack", Account: "acme", Conversation: "#random", Date: "9999999999", Thread: true,
			Msg: modelv1.MsgLine{ID: "D1", Ts: now.Add(-72 * time.Hour)}},
	}

	filtered := FilterThreadsBySince(matches, 24*time.Hour)

	if len(filtered) != 3 {
		t.Fatalf("filtered = %d, want 3 (1 date + 2 alive thread)", len(filtered))
	}
	ids := map[string]bool{}
	for _, m := range filtered {
		ids[m.Msg.ID] = true
	}
	if !ids["M1"] || !ids["T1"] || !ids["T2"] {
		t.Errorf("expected M1, T1, T2; got %v", ids)
	}
	if ids["D1"] {
		t.Error("dead thread match D1 should have been filtered out")
	}
}

func TestFilterThreadsBySince_KeepsAllNonThread(t *testing.T) {
	now := time.Now()
	matches := []Match{
		{Date: "2026-03-16", Msg: modelv1.MsgLine{ID: "M1", Ts: now.Add(-1 * time.Hour)}},
		{Date: "2026-03-15", Msg: modelv1.MsgLine{ID: "M2", Ts: now.Add(-48 * time.Hour)}},
	}
	filtered := FilterThreadsBySince(matches, 24*time.Hour)
	if len(filtered) != 2 {
		t.Errorf("filtered = %d, want 2", len(filtered))
	}
}

func TestParseFilePath_WhatsApp(t *testing.T) {
	plat, acct, conv, date, _, _ := ParseFilePath("/data/whatsapp/15551234567/+14155551234/2026-03-16.jsonl", "/data")
	if plat != "whatsapp" || acct != "15551234567" || conv != "+14155551234" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q)", plat, acct, conv, date)
	}
}

// TestParseFilePath_ConversationNamedThreads verifies that a date file
// under a conversation literally named "threads" is parsed correctly:
// the conversation is "threads" and the thread flag is false.
func TestParseFilePath_ConversationNamedThreads(t *testing.T) {
	plat, acct, conv, date, thread, err := ParseFilePath(
		"/data/slack/acme-corp/threads/2026-04-06.jsonl",
		"/data",
	)
	if err != nil {
		t.Fatalf("ParseFilePath: %v", err)
	}
	if plat != "slack" || acct != "acme-corp" || conv != "threads" || date != "2026-04-06" {
		t.Errorf("got (%q, %q, %q, %q), want (slack, acme-corp, threads, 2026-04-06)",
			plat, acct, conv, date)
	}
	if thread {
		t.Error("thread = true, want false for date file under conversation named threads")
	}
}

// TestParseFilePath_ThreadFile_InConvNamedThreads verifies that a real
// thread file under a conversation literally named "threads" is still
// correctly identified (conv/threads/threads/TS.jsonl).
func TestParseFilePath_ThreadFile_InConvNamedThreads(t *testing.T) {
	plat, acct, conv, date, thread, err := ParseFilePath(
		"/data/slack/acme-corp/threads/threads/1711568940.789012.jsonl",
		"/data",
	)
	if err != nil {
		t.Fatalf("ParseFilePath: %v", err)
	}
	if plat != "slack" || acct != "acme-corp" || conv != "threads" || date != "1711568940.789012" {
		t.Errorf("got (%q, %q, %q, %q), want (slack, acme-corp, threads, 1711568940.789012)",
			plat, acct, conv, date)
	}
	if !thread {
		t.Error("thread = false, want true")
	}
}
