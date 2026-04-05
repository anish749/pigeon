package storev1

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func setup(t *testing.T) (*FSStore, account.Account) {
	t.Helper()
	dir := t.TempDir()
	store := NewFSStore(dir)
	acct := account.New("slack", "acme-corp")
	return store, acct
}

func ts(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

func msgLine(id string, t time.Time, sender, senderID, text string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: id, Ts: t, Sender: sender, SenderID: senderID, Text: text,
		},
	}
}

func reactLine(t time.Time, msgID, sender, senderID, emoji string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineReaction,
		React: &modelv1.ReactLine{
			Ts: t, MsgID: msgID, Sender: sender, SenderID: senderID, Emoji: emoji,
		},
	}
}

// --- Append + Read round-trip ---

func TestAppendAndRead(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	m2 := msgLine("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "world")
	r1 := reactLine(ts(2026, 3, 16, 9, 2, 0), "M1", "Bob", "U2", "thumbsup")

	for _, line := range []modelv1.Line{m1, m2, r1} {
		if err := s.Append(acct, "#general", line); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(df.Messages))
	}
	// Reaction should be on the first message (M1)
	if len(df.Messages[0].Reactions) != 1 {
		t.Errorf("M1 reactions = %d, want 1", len(df.Messages[0].Reactions))
	}
}

func TestAppendAndRead_MultiplesDays(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 15, 9, 0, 0), "Alice", "U1", "yesterday")
	m2 := msgLine("M2", ts(2026, 3, 16, 9, 0, 0), "Bob", "U2", "today")

	if err := s.Append(acct, "#general", m1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(acct, "#general", m2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Read specific date
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-15"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 1 || df.Messages[0].ID != "M1" {
		t.Errorf("expected M1 for 2026-03-15, got %d messages", len(df.Messages))
	}
}

func TestAppend_DedupOnRead(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	// Append same message twice
	s.Append(acct, "#general", m1)
	s.Append(acct, "#general", m1)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	// Compaction deduplicates
	if len(df.Messages) != 1 {
		t.Errorf("messages = %d, want 1 (deduped)", len(df.Messages))
	}
}

// --- Thread ---

func TestAppendThreadAndRead(t *testing.T) {
	s, acct := setup(t)

	parent := msgLine("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread start")
	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0),
			Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true,
		},
	}
	sep := modelv1.Line{Type: modelv1.LineSeparator}

	s.AppendThread(acct, "#general", "P1", parent)
	s.AppendThread(acct, "#general", "P1", reply)
	s.AppendThread(acct, "#general", "P1", sep)

	tf, err := s.ReadThread(acct, "#general", "P1")
	if err != nil {
		t.Fatalf("ReadThread: %v", err)
	}
	if tf == nil {
		t.Fatal("ReadThread returned nil")
	}
	if tf.Parent.ID != "P1" {
		t.Errorf("parent ID = %q, want P1", tf.Parent.ID)
	}
	if len(tf.Replies) != 1 {
		t.Errorf("replies = %d, want 1", len(tf.Replies))
	}
}

func TestReadThread_NotFound(t *testing.T) {
	s, acct := setup(t)

	tf, err := s.ReadThread(acct, "#general", "nonexistent")
	if err != nil {
		t.Fatalf("ReadThread: %v", err)
	}
	if tf != nil {
		t.Error("expected nil for nonexistent thread")
	}
}

// --- List ---

func TestListPlatformsAndAccounts(t *testing.T) {
	s, acct := setup(t)

	// Create a conversation to populate the directory structure
	m := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	s.Append(acct, "#general", m)

	platforms, err := s.ListPlatforms()
	if err != nil {
		t.Fatalf("ListPlatforms: %v", err)
	}
	if len(platforms) != 1 || platforms[0] != "slack" {
		t.Errorf("platforms = %v, want [slack]", platforms)
	}

	accounts, err := s.ListAccounts("slack")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0] != "acme-corp" {
		t.Errorf("accounts = %v, want [acme-corp]", accounts)
	}

	convs, err := s.ListConversations(acct)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 || convs[0] != "#general" {
		t.Errorf("conversations = %v, want [#general]", convs)
	}
}

// --- Search ---

func TestSearch(t *testing.T) {
	s, acct := setup(t)

	s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "deploy is done"))
	s.Append(acct, "#general", msgLine("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "nice work"))
	s.Append(acct, "#random", msgLine("M3", ts(2026, 3, 16, 9, 2, 0), "Alice", "U1", "deploy the new version"))

	results, err := s.Search("deploy", SearchOpts{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results = %d, want 2 (one per conversation)", len(results))
	}

	total := 0
	for _, r := range results {
		total += r.MatchCount
	}
	if total != 2 {
		t.Errorf("total matches = %d, want 2", total)
	}
}

func TestSearch_NoResults(t *testing.T) {
	s, acct := setup(t)
	s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello"))

	results, err := s.Search("nonexistent", SearchOpts{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

// --- Maintain ---

func TestMaintain(t *testing.T) {
	s, acct := setup(t)

	// Append duplicate messages and a react/unreact pair
	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	s.Append(acct, "#general", m1)
	s.Append(acct, "#general", m1) // duplicate

	r := reactLine(ts(2026, 3, 16, 9, 1, 0), "M1", "Bob", "U2", "thumbsup")
	s.Append(acct, "#general", r)
	unreact := modelv1.Line{
		Type: modelv1.LineUnreaction,
		React: &modelv1.ReactLine{
			Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1",
			Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true,
		},
	}
	s.Append(acct, "#general", unreact)

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	// Read the raw file to verify it was compacted
	dir := filepath.Join(s.base, "slack", "acme-corp", "#general")
	data, err := os.ReadFile(filepath.Join(dir, "2026-03-16.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	df, _ := modelv1.ParseDateFile(data)
	if len(df.Messages) != 1 {
		t.Errorf("after maintenance: messages = %d, want 1 (deduped)", len(df.Messages))
	}
	if len(df.Reactions) != 0 {
		t.Errorf("after maintenance: reactions = %d, want 0 (react+unreact reconciled)", len(df.Reactions))
	}

	// Verify maintenance state file exists
	stateFile := filepath.Join(s.base, "slack", "acme-corp", ".maintenance.json")
	if !fileExists(stateFile) {
		t.Error("maintenance state file not created")
	}
}

func TestMaintain_SkipsUnchangedFiles(t *testing.T) {
	s, acct := setup(t)
	s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello"))

	// First maintenance
	s.Maintain(acct)

	// Get mtime of file after first maintenance
	dir := filepath.Join(s.base, "slack", "acme-corp", "#general")
	info1, _ := os.Stat(filepath.Join(dir, "2026-03-16.txt"))

	// Second maintenance (no changes) should not rewrite
	s.Maintain(acct)

	info2, _ := os.Stat(filepath.Join(dir, "2026-03-16.txt"))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("maintenance rewrote file that hadn't changed")
	}
}

// --- ReadConversation with empty store ---

func TestReadConversation_Empty(t *testing.T) {
	s, acct := setup(t)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 0 {
		t.Errorf("messages = %d, want 0", len(df.Messages))
	}
}
