package search

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// These tests write real JSONL files, run actual rg/grep against them,
// and parse the real output — testing the full pipeline, not assumptions
// about what rg/grep produce.

func writeJSONL(t *testing.T, path string, lines []modelv1.Line) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		data, err := modelv1.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		f.Write(data)
		f.Write([]byte("\n"))
	}
}

func msg(id string, t time.Time, sender, senderID, text string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: id, Ts: t, Sender: sender, SenderID: senderID, Text: text,
		},
	}
}

func react(t time.Time, msgID, sender, senderID, emoji string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineReaction,
		React: &modelv1.ReactLine{
			Ts: t, MsgID: msgID, Sender: sender, SenderID: senderID, Emoji: emoji,
		},
	}
}

func requireRg(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg not available")
	}
	return p
}

func runRg(t *testing.T, rgPath, query, dir string) []byte {
	t.Helper()
	out, err := exec.Command(rgPath, "--color=never", query, dir).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("rg: %v", err)
	}
	return out
}

func runRgContext(t *testing.T, rgPath, query, dir string, context int) []byte {
	t.Helper()
	out, err := exec.Command(rgPath, "--color=never", fmt.Sprintf("-C%d", context), query, dir).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("rg -C: %v", err)
	}
	return out
}

// --- Integration tests ---

func TestIntegration_RgBasicSearch(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"),
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "nice work"),
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Alice", "U1", "deploy the hotfix too"),
	})

	output := runRg(t, rgPath, "deploy", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	if matches[0].Msg.ID != "M1" {
		t.Errorf("match[0].ID = %q, want M1", matches[0].Msg.ID)
	}
	if matches[0].Platform != "slack" {
		t.Errorf("match[0].Platform = %q, want slack", matches[0].Platform)
	}
	if matches[0].Account != "acme" {
		t.Errorf("match[0].Account = %q, want acme", matches[0].Account)
	}
	if matches[0].Conversation != "#general" {
		t.Errorf("match[0].Conversation = %q, want #general", matches[0].Conversation)
	}
	if matches[0].Date != "2026-03-16" {
		t.Errorf("match[0].Date = %q, want 2026-03-16", matches[0].Date)
	}
}

func TestIntegration_RgSkipsNonMessageEvents(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello world"),
		react(ts(2026, 3, 16, 9, 1, 0), "M1", "Bob", "U2", "thumbsup"),
	})

	// "hello" matches msg, "thumbsup" matches react — but ParseGrepOutput only returns msgs
	output := runRg(t, rgPath, "hello\\|thumbsup", dir)
	// rg doesn't support \| — use a different approach
	output = runRg(t, rgPath, ".", dir) // match all lines
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	// Only the message should be in matches, not the reaction
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1 (only msg events)", len(matches))
	}
}

func TestIntegration_RgMultipleConversations(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy general"),
	})
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#random", "2026-03-16.txt"), []modelv1.Line{
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy random"),
	})
	writeJSONL(t, filepath.Join(dir, "whatsapp", "15551234567", "+14155551234", "2026-03-16.txt"), []modelv1.Line{
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "C1", "deploy whatsapp"),
	})

	output := runRg(t, rgPath, "deploy", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(matches))
	}

	platforms := map[string]bool{}
	for _, m := range matches {
		platforms[m.Platform] = true
	}
	if !platforms["slack"] || !platforms["whatsapp"] {
		t.Errorf("platforms = %v, want slack and whatsapp", platforms)
	}
}

func TestIntegration_RgThreadFiles(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "threads", "1711568940.789012.txt"), []modelv1.Line{
		msg("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy thread parent"),
		msg("R1", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy reply"),
	})

	output := runRg(t, rgPath, "deploy", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	// Thread files should still resolve to the conversation, not "threads"
	if matches[0].Conversation != "#general" {
		t.Errorf("match[0].Conversation = %q, want #general", matches[0].Conversation)
	}
}

func TestIntegration_RgWithContext(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "before"),
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy is done"),
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "U3", "after"),
	})

	output := runRgContext(t, rgPath, "deploy", dir, 1)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	// With -C 1, rg returns the match plus 1 line before and after
	// All three are message events, so all should parse
	if len(matches) != 3 {
		t.Errorf("matches = %d, want 3 (match + context)", len(matches))
	}
}

func TestIntegration_RgTextWithBraces(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "meeting at {office} tomorrow"),
	})

	output := runRg(t, rgPath, "office", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Msg.Text != "meeting at {office} tomorrow" {
		t.Errorf("text = %q, want 'meeting at {office} tomorrow'", matches[0].Msg.Text)
	}
}

func TestIntegration_RgTextWithNewlines(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "line one\nline two\nline three"),
	})

	output := runRg(t, rgPath, "line one", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Msg.Text != "line one\nline two\nline three" {
		t.Errorf("text = %q, want multiline text", matches[0].Msg.Text)
	}
}

func TestIntegration_RgPreservesAllFields(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	m := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1",
			Via: modelv1.ViaPigeonAsUser, Text: "searchable text",
			ReplyTo: "Q1", Reply: true,
			Attachments: []modelv1.Attachment{{ID: "F1", Type: "image/jpeg"}},
		},
	}
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{m})

	output := runRg(t, rgPath, "searchable", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}

	got := matches[0].Msg
	if got.ID != "M1" || got.Sender != "Alice" || got.SenderID != "U1" {
		t.Errorf("basic fields: %+v", got)
	}
	if got.Via != modelv1.ViaPigeonAsUser {
		t.Errorf("Via = %q", got.Via)
	}
	if got.ReplyTo != "Q1" || !got.Reply {
		t.Errorf("ReplyTo=%q Reply=%v", got.ReplyTo, got.Reply)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != "F1" {
		t.Errorf("Attachments = %+v", got.Attachments)
	}
}

func TestIntegration_RgNoMatches(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello world"),
	})

	output := runRg(t, rgPath, "nonexistent", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("matches = %d, want 0", len(matches))
	}
}

func TestIntegration_RgSenderWithSpecialChars(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	// Colons in sender names — the old bracket format couldn't handle this
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Dr. Smith: Cardiologist", "U1", "searchme"),
	})

	output := runRg(t, rgPath, "searchme", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Msg.Sender != "Dr. Smith: Cardiologist" {
		t.Errorf("sender = %q, want 'Dr. Smith: Cardiologist'", matches[0].Msg.Sender)
	}
}

func TestIntegration_RgOutputIsValidJSON(t *testing.T) {
	rgPath := requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.txt"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "test message"),
	})

	output := runRg(t, rgPath, "test", dir)
	// Verify that the JSON portion of rg output is valid
	for _, line := range splitLines(output) {
		idx := indexOf(line, ":{")
		if idx < 0 {
			continue
		}
		jsonPart := line[idx+1:]
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(jsonPart), &raw); err != nil {
			t.Errorf("rg output line is not valid JSON: %s", jsonPart)
		}
	}
}

// helpers for the integration test
func splitLines(data []byte) []string {
	var lines []string
	for _, line := range filepath.SplitList(string(data)) {
		lines = append(lines, line)
	}
	// Actually just split on newline
	s := string(data)
	if s == "" {
		return nil
	}
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
