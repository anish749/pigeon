package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", dir)
	return dir
}

func TestWriteThreadMessage(t *testing.T) {
	setupTestDataDir(t)

	ts := time.Date(2026, 3, 27, 19, 48, 58, 0, time.FixedZone("CET", 3600))

	// Write parent
	if err := WriteThreadMessage("slack", "acme", "#general", "1711568938.123456", "Anish", "Does TVR depend on views?", ts, false); err != nil {
		t.Fatal(err)
	}

	// Write reply (indented)
	replyTS := time.Date(2026, 3, 27, 21, 9, 34, 0, time.FixedZone("CET", 3600))
	if err := WriteThreadMessage("slack", "acme", "#general", "1711568938.123456", "ally", "do we have a sense of distribution?", replyTS, true); err != nil {
		t.Fatal(err)
	}

	// Read back
	lines, err := ReadThread("slack", "acme", "#general", "1711568938.123456")
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	// Parent should not be indented
	if strings.HasPrefix(lines[0], "  ") {
		t.Errorf("parent should not be indented: %q", lines[0])
	}
	if !strings.Contains(lines[0], "Anish") || !strings.Contains(lines[0], "TVR") {
		t.Errorf("unexpected parent line: %q", lines[0])
	}

	// Reply should be indented
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("reply should be indented: %q", lines[1])
	}
	if !strings.Contains(lines[1], "ally") || !strings.Contains(lines[1], "distribution") {
		t.Errorf("unexpected reply line: %q", lines[1])
	}
}

func TestWriteThreadContext(t *testing.T) {
	setupTestDataDir(t)

	parentTS := time.Date(2026, 3, 27, 12, 14, 55, 0, time.FixedZone("CET", 3600))

	// Write parent
	WriteThreadMessage("slack", "acme", "#support", "1711568000.000000", "Anish", "question about coverage", parentTS, false)

	// Write reply
	replyTS := time.Date(2026, 3, 27, 14, 30, 0, 0, time.FixedZone("CET", 3600))
	WriteThreadMessage("slack", "acme", "#support", "1711568000.000000", "bellis", "It depends on the tier", replyTS, true)

	// Write context separator + context messages
	EnsureThreadContextSeparator("slack", "acme", "#support", "1711568000.000000")

	ctxTS := time.Date(2026, 3, 27, 12, 16, 0, 0, time.FixedZone("CET", 3600))
	WriteThreadContext("slack", "acme", "#support", "1711568000.000000", "Grant", "Follow up from Sony", ctxTS)

	lines, err := ReadThread("slack", "acme", "#support", "1711568000.000000")
	if err != nil {
		t.Fatal(err)
	}

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}

	// Line 0: parent (unindented)
	if strings.HasPrefix(lines[0], "  ") {
		t.Error("parent should not be indented")
	}
	// Line 1: reply (indented)
	if !strings.HasPrefix(lines[1], "  ") {
		t.Error("reply should be indented")
	}
	// Line 2: separator
	if lines[2] != "--- channel context ---" {
		t.Errorf("expected separator, got: %q", lines[2])
	}
	// Line 3: context message (unindented)
	if strings.HasPrefix(lines[3], "  ") {
		t.Error("context message should not be indented")
	}
	if !strings.Contains(lines[3], "Grant") {
		t.Errorf("expected Grant in context, got: %q", lines[3])
	}
}

func TestDeduplication(t *testing.T) {
	setupTestDataDir(t)

	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.FixedZone("CET", 3600))

	// Write same message twice
	WriteThreadMessage("slack", "acme", "#general", "123.456", "Alice", "hello", ts, false)
	WriteThreadMessage("slack", "acme", "#general", "123.456", "Alice", "hello", ts, false)

	lines, _ := ReadThread("slack", "acme", "#general", "123.456")
	if len(lines) != 1 {
		t.Errorf("expected 1 line after dedup, got %d", len(lines))
	}
}

func TestInterleaveThreads(t *testing.T) {
	setupTestDataDir(t)

	// Write channel messages to a date file
	ts1 := time.Date(2026, 3, 27, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	ts2 := time.Date(2026, 3, 27, 11, 0, 0, 0, time.FixedZone("CET", 3600))
	ts3 := time.Date(2026, 3, 27, 12, 0, 0, 0, time.FixedZone("CET", 3600))

	WriteMessage("slack", "acme", "#general", "Alice", "first message", ts1)
	WriteMessage("slack", "acme", "#general", "Bob", "thread parent", ts2)
	WriteMessage("slack", "acme", "#general", "Charlie", "unrelated message", ts3)

	// Write a thread file where Bob's message is the parent
	// The parent line in the thread file must match exactly what's in the date file
	parentLine := "[2026-03-27 11:00:00 +01:00] Bob: thread parent"
	threadTS := "1711533600.000000"

	threadDir := ThreadDir("slack", "acme", "#general")
	os.MkdirAll(threadDir, 0755)
	threadFile := filepath.Join(threadDir, threadTS+".txt")

	replyLine := "  [2026-03-27 13:00:00 +01:00] Dave: reply in thread"
	os.WriteFile(threadFile, []byte(parentLine+"\n"+replyLine+"\n--- channel context ---\n[2026-03-27 12:00:00 +01:00] Charlie: unrelated message\n"), 0644)

	// Read channel messages
	lines, err := ReadMessages("slack", "acme", "#general", ReadOpts{Date: "2026-03-27"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 channel lines, got %d", len(lines))
	}

	// Interleave threads
	enriched := InterleaveThreads("slack", "acme", "#general", lines)

	// Should now have: Alice's msg, Bob's msg, reply, separator, context, Charlie's msg
	if len(enriched) != 6 {
		t.Fatalf("expected 6 lines after interleave, got %d:\n%s", len(enriched), strings.Join(enriched, "\n"))
	}

	if !strings.Contains(enriched[0], "Alice") {
		t.Errorf("line 0 should be Alice: %q", enriched[0])
	}
	if !strings.Contains(enriched[1], "Bob") {
		t.Errorf("line 1 should be Bob (parent): %q", enriched[1])
	}
	if !strings.Contains(enriched[2], "Dave") || !strings.HasPrefix(enriched[2], "  ") {
		t.Errorf("line 2 should be indented Dave reply: %q", enriched[2])
	}
	if enriched[3] != "--- channel context ---" {
		t.Errorf("line 3 should be separator: %q", enriched[3])
	}
	if !strings.Contains(enriched[5], "Charlie") {
		t.Errorf("line 5 should be Charlie: %q", enriched[5])
	}
}

func TestSearchIncludesThreads(t *testing.T) {
	setupTestDataDir(t)

	// Write a channel message
	ts := time.Date(2026, 3, 27, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	WriteMessage("slack", "acme", "#general", "Alice", "channel message", ts)

	// Write a thread file with searchable content
	threadDir := ThreadDir("slack", "acme", "#general")
	os.MkdirAll(threadDir, 0755)
	threadFile := filepath.Join(threadDir, "123.456.txt")
	os.WriteFile(threadFile, []byte("[2026-03-27 10:00:00 +01:00] Alice: channel message\n  [2026-03-27 11:00:00 +01:00] Bob: secret thread content\n"), 0644)

	// Search for something only in the thread
	results, err := SearchMessages("secret thread", "slack", "acme", 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected search to find content in thread file")
	}

	found := false
	for _, r := range results {
		if strings.HasPrefix(r.Date, "thread:") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected result with thread: prefix in Date field")
	}
}

func TestSearchSinceIncludesRecentlyActiveOldThread(t *testing.T) {
	setupTestDataDir(t)

	threadDir := ThreadDir("slack", "acme", "#general")
	if err := os.MkdirAll(threadDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldParent := time.Now().Add(-48 * time.Hour)
	recentReply := time.Now().Add(-30 * time.Minute)
	threadFile := filepath.Join(threadDir, "123.456.txt")
	content := fmt.Sprintf("[%s] Alice: old parent\n  [%s] Bob: recent reply with deploy keyword\n",
		oldParent.Format("2006-01-02 15:04:05 -07:00"),
		recentReply.Format("2006-01-02 15:04:05 -07:00"),
	)
	if err := os.WriteFile(threadFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := SearchMessages("deploy keyword", "slack", "acme", 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected search --since to find recently active thread")
	}

	found := false
	for _, r := range results {
		if strings.HasPrefix(r.Date, "thread:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a thread search result for recently active old thread")
	}
}

func TestListConversationsExcludesThreadsDir(t *testing.T) {
	dir := setupTestDataDir(t)

	// Create conversation dirs and a threads dir
	os.MkdirAll(filepath.Join(dir, "slack", "acme", "#general"), 0755)
	os.MkdirAll(filepath.Join(dir, "slack", "acme", "#general", "threads"), 0755)
	os.MkdirAll(filepath.Join(dir, "slack", "acme", "@dave"), 0755)

	convs, err := ListConversations("slack", "acme", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range convs {
		if c.DirName == "threads" {
			t.Error("threads directory should not appear as a conversation")
		}
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(convs))
	}
}

func TestListThreads(t *testing.T) {
	setupTestDataDir(t)

	// No threads dir yet
	threads, err := ListThreads("slack", "acme", "#general")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(threads))
	}

	// Create thread files
	threadDir := ThreadDir("slack", "acme", "#general")
	os.MkdirAll(threadDir, 0755)
	os.WriteFile(filepath.Join(threadDir, "111.222.txt"), []byte("parent\n"), 0644)
	os.WriteFile(filepath.Join(threadDir, "333.444.txt"), []byte("parent\n"), 0644)

	threads, err = ListThreads("slack", "acme", "#general")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Errorf("expected 2 threads, got %d", len(threads))
	}
}
