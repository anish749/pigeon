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

// --- ParseGrepOutput ---

func TestParseGrepOutput_BasicMessages(t *testing.T) {
	lines := []string{
		"/data/slack/acme-corp/#general/2026-03-16.txt:" + msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"),
		"/data/slack/acme-corp/#general/2026-03-16.txt:" + msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "nice deploy"),
	}
	output := []byte(strings.Join(lines, "\n"))

	matches := ParseGrepOutput(output, "/data")
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
	lines := []string{
		"/data/slack/acme/ch/2026-03-16.txt:" + msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello"),
		"/data/slack/acme/ch/2026-03-16.txt:" + reactJSON(ts(2026, 3, 16, 9, 1, 0), "M1", "Bob", "U2", "thumbsup"),
		"/data/slack/acme/ch/2026-03-16.txt:" + `{"type":"edit","ts":"2026-03-16T09:02:00Z","msg":"M1","sender":"Alice","from":"U1","text":"updated"}`,
		"/data/slack/acme/ch/2026-03-16.txt:" + `{"type":"delete","ts":"2026-03-16T09:03:00Z","msg":"M1","sender":"Alice","from":"U1"}`,
	}
	output := []byte(strings.Join(lines, "\n"))
	matches := ParseGrepOutput(output, "/data")
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1 (only msg events)", len(matches))
	}
}

func TestParseGrepOutput_SkipsContextSeparators(t *testing.T) {
	lines := []string{
		"/data/slack/acme/ch/2026-03-16.txt:" + msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "first"),
		"--",
		"/data/slack/acme/ch/2026-03-16.txt:" + msgJSON("M2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "second"),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches := ParseGrepOutput(output, "/data")
	if len(matches) != 2 {
		t.Errorf("matches = %d, want 2", len(matches))
	}
}

func TestParseGrepOutput_SkipsGarbageLines(t *testing.T) {
	lines := []string{
		"this is not valid output",
		"/data/slack/acme/ch/2026-03-16.txt:" + msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello"),
		"another garbage line",
	}
	output := []byte(strings.Join(lines, "\n"))
	matches := ParseGrepOutput(output, "/data")
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1", len(matches))
	}
}

func TestParseGrepOutput_EmptyOutput(t *testing.T) {
	matches := ParseGrepOutput(nil, "/data")
	if len(matches) != 0 {
		t.Errorf("matches = %d, want 0", len(matches))
	}
}

func TestParseGrepOutput_MultipleConversations(t *testing.T) {
	lines := []string{
		"/data/slack/acme/#general/2026-03-16.txt:" + msgJSON("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy"),
		"/data/slack/acme/#random/2026-03-16.txt:" + msgJSON("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy too"),
		"/data/whatsapp/15551234567/+14155551234/2026-03-16.txt:" + msgJSON("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "C1", "deploy three"),
	}
	output := []byte(strings.Join(lines, "\n"))
	matches := ParseGrepOutput(output, "/data")
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
	line := fmt.Sprintf("/data/slack/acme/ch/2026-03-16.txt:%s", jsonLine(msg))
	matches := ParseGrepOutput([]byte(line), "/data")
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
	plat, acct, conv, date := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.txt:", "/data")
	if plat != "slack" || acct != "acme-corp" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (slack, acme-corp, #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_PlatformScope(t *testing.T) {
	plat, acct, conv, date := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.txt:", "/data/slack")
	if plat != "" || acct != "acme-corp" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (, acme-corp, #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_AccountScope(t *testing.T) {
	plat, acct, conv, date := ParseFilePath("/data/slack/acme-corp/#general/2026-03-16.txt:", "/data/slack/acme-corp")
	if plat != "" || acct != "" || conv != "#general" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q), want (, , #general, 2026-03-16)", plat, acct, conv, date)
	}
}

func TestParseFilePath_WhatsApp(t *testing.T) {
	plat, acct, conv, date := ParseFilePath("/data/whatsapp/15551234567/+14155551234/2026-03-16.txt:", "/data")
	if plat != "whatsapp" || acct != "15551234567" || conv != "+14155551234" || date != "2026-03-16" {
		t.Errorf("got (%q, %q, %q, %q)", plat, acct, conv, date)
	}
}
