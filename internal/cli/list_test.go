package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeJSONL creates a JSONL file with a single message line at the given timestamp.
func writeJSONL(t *testing.T, path string, ts time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"msg","id":"test","ts":"%s","sender":"Test","from":"U123","text":"hello"}`, ts.Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractConversations(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	generalDate1 := filepath.Join(root, "slack/acme/#general/2026-04-07.jsonl")
	generalDate2 := filepath.Join(root, "slack/acme/#general/2026-04-06.jsonl")
	generalThread := filepath.Join(root, "slack/acme/#general/threads/1742100000.jsonl")
	randomDate := filepath.Join(root, "slack/acme/#random/2026-04-07.jsonl")
	aliceDate := filepath.Join(root, "whatsapp/phone/Alice/2026-04-05.jsonl")

	writeJSONL(t, generalDate1, now.Add(-1*time.Hour))
	writeJSONL(t, generalDate2, now.Add(-24*time.Hour))
	writeJSONL(t, generalThread, now.Add(-30*time.Minute))
	writeJSONL(t, randomDate, now.Add(-2*time.Hour))
	writeJSONL(t, aliceDate, now.Add(-3*time.Hour))

	files := []string{generalDate1, generalDate2, generalThread, randomDate, aliceDate}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 3 {
		t.Fatalf("got %d conversations, want 3", len(convs))
	}

	// #general: thread has newest message (30m ago), should win over date file (1h ago).
	if convs[0].Display != "slack/acme/#general" {
		t.Errorf("convs[0].Display = %q, want slack/acme/#general", convs[0].Display)
	}
	age := now.Sub(convs[0].LatestTime)
	if age < 25*time.Minute || age > 35*time.Minute {
		t.Errorf("convs[0].LatestTime age = %v, want ~30m", age)
	}
	if convs[0].Dir != filepath.Join(root, "slack/acme/#general") {
		t.Errorf("convs[0].Dir = %q, want %s", convs[0].Dir, filepath.Join(root, "slack/acme/#general"))
	}

	// #random: single date file.
	if convs[1].Display != "slack/acme/#random" {
		t.Errorf("convs[1].Display = %q, want slack/acme/#random", convs[1].Display)
	}

	// Alice: whatsapp conversation.
	if convs[2].Display != "whatsapp/phone/Alice" {
		t.Errorf("convs[2].Display = %q, want whatsapp/phone/Alice", convs[2].Display)
	}
}

func TestExtractConversations_ThreadOnly(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	thread1 := filepath.Join(root, "slack/acme/#general/threads/1742100000.jsonl")
	thread2 := filepath.Join(root, "slack/acme/#general/threads/1742200000.jsonl")

	writeJSONL(t, thread1, now.Add(-2*time.Hour))
	writeJSONL(t, thread2, now.Add(-1*time.Hour))

	files := []string{thread1, thread2}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	// Thread-only conversation should have timestamp from newest thread.
	age := now.Sub(convs[0].LatestTime)
	if age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("thread-only LatestTime age = %v, want ~1h", age)
	}
}

func TestExtractConversations_Empty(t *testing.T) {
	convs, err := extractConversations(nil, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 0 {
		t.Errorf("got %d conversations, want 0", len(convs))
	}
}

// TestExtractConversations_ConversationNamedThreads verifies that a
// conversation literally named "threads" is not dropped by the
// path-component strip logic.
func TestExtractConversations_ConversationNamedThreads(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	dateFile := filepath.Join(root, "slack/acme/threads/2026-04-07.jsonl")
	threadFile := filepath.Join(root, "slack/acme/threads/threads/1742100000.jsonl")

	writeJSONL(t, dateFile, now.Add(-1*time.Hour))
	writeJSONL(t, threadFile, now.Add(-2*time.Hour))

	files := []string{dateFile, threadFile}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1: %+v", len(convs), convs)
	}
	if convs[0].Display != "slack/acme/threads" {
		t.Errorf("Display = %q, want slack/acme/threads", convs[0].Display)
	}
	if convs[0].Dir != filepath.Join(root, "slack/acme/threads") {
		t.Errorf("Dir = %q, want %s", convs[0].Dir, filepath.Join(root, "slack/acme/threads"))
	}
	// Date file is newer (1h ago vs 2h ago), should be picked.
	age := now.Sub(convs[0].LatestTime)
	if age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("LatestTime age = %v, want ~1h", age)
	}
}

func TestExtractConversations_PreservesOrder(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	beta := filepath.Join(root, "slack/acme/#beta/2026-04-07.jsonl")
	alpha := filepath.Join(root, "slack/acme/#alpha/2026-04-07.jsonl")

	writeJSONL(t, beta, now.Add(-1*time.Hour))
	writeJSONL(t, alpha, now.Add(-2*time.Hour))

	files := []string{beta, alpha}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convs))
	}
	// Order should match first-seen in the input.
	if convs[0].Display != "slack/acme/#beta" {
		t.Errorf("first conversation = %q, want slack/acme/#beta", convs[0].Display)
	}
}
