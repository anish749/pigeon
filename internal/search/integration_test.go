package search

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// These tests write real JSONL files, run actual rg --json against them,
// and parse the real output — testing the full pipeline.

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

func requireRg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
}

func runRgJSON(t *testing.T, query, dir string, extraArgs ...string) []byte {
	t.Helper()
	args := append([]string{"--json"}, extraArgs...)
	args = append(args, query, dir)
	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("rg --json: %v", err)
	}
	return out
}

// --- Integration tests ---

func TestIntegration_RgBasicSearch(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"),
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "nice work"),
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Alice", "U1", "deploy the hotfix too"),
	})

	output := runRgJSON(t, "deploy", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	if matches[0].Line.Msg.ID != "M1" {
		t.Errorf("match[0].ID = %q, want M1", matches[0].Line.Msg.ID)
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
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello world"),
		react(ts(2026, 3, 16, 9, 1, 0), "M1", "Bob", "U2", "thumbsup"),
	})

	output := runRgJSON(t, ".", dir) // match all lines
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1 (only msg events)", len(matches))
	}
}

func TestIntegration_RgMultipleConversations(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy general"),
	})
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#random", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy random"),
	})
	writeJSONL(t, filepath.Join(dir, "whatsapp", "15551234567", "+14155551234", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "C1", "deploy whatsapp"),
	})

	output := runRgJSON(t, "deploy", dir)
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
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "threads", "1711568940.789012.jsonl"), []modelv1.Line{
		msg("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy thread parent"),
		msg("R1", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy reply"),
	})

	output := runRgJSON(t, "deploy", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	if matches[0].Conversation != "#general" {
		t.Errorf("match[0].Conversation = %q, want #general", matches[0].Conversation)
	}
}

func TestIntegration_RgWithContext(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "before"),
		msg("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "deploy is done"),
		msg("M3", ts(2026, 3, 16, 9, 2, 0), "Charlie", "U3", "after"),
	})

	output := runRgJSON(t, "deploy", dir, "-C1")
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 3 {
		t.Errorf("matches = %d, want 3 (match + context)", len(matches))
	}
}

func TestIntegration_RgTextWithBraces(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "meeting at {office} tomorrow"),
	})

	output := runRgJSON(t, "office", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Line.Msg.Text != "meeting at {office} tomorrow" {
		t.Errorf("text = %q, want 'meeting at {office} tomorrow'", matches[0].Line.Msg.Text)
	}
}

func TestIntegration_RgTextWithNewlines(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "line one\nline two\nline three"),
	})

	output := runRgJSON(t, "line one", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Line.Msg.Text != "line one\nline two\nline three" {
		t.Errorf("text = %q, want multiline text", matches[0].Line.Msg.Text)
	}
}

func TestIntegration_RgPreservesAllFields(t *testing.T) {
	requireRg(t)
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
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{m})

	output := runRgJSON(t, "searchable", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}

	got := matches[0].Line.Msg
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
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello world"),
	})

	output := runRgJSON(t, "nonexistent", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("matches = %d, want 0", len(matches))
	}
}

func TestIntegration_RgSenderWithSpecialChars(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Dr. Smith: Cardiologist", "U1", "searchme"),
	})

	output := runRgJSON(t, "searchme", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Fatalf("ParseGrepOutput: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Line.Msg.Sender != "Dr. Smith: Cardiologist" {
		t.Errorf("sender = %q, want 'Dr. Smith: Cardiologist'", matches[0].Line.Msg.Sender)
	}
}

func TestIntegration_RgEmbeddedJSON(t *testing.T) {
	requireRg(t)
	dir := t.TempDir()

	// This was the original bug — embedded JSON in message text caused
	// the old :{ delimiter split to fail.
	writeJSONL(t, filepath.Join(dir, "slack", "acme", "#general", "2026-03-16.jsonl"), []modelv1.Line{
		msg("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", fmt.Sprintf(`query: {"field":"value","nested":{"a":1}}`)),
	})

	output := runRgJSON(t, "query", dir)
	matches, err := ParseGrepOutput(output, dir)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
}
