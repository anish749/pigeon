package store

import (
	"os"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func setup(t *testing.T) (*FSStore, account.Account) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	store := NewFSStore(root)
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
	conv := s.convDir(acct, "#general")
	data, err := os.ReadFile(conv.DateFile("2026-03-16"))
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
	stateFile := s.root.AccountFor(acct).MaintenancePath()
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
	dateFile := s.convDir(acct, "#general").DateFile("2026-03-16")
	info1, _ := os.Stat(dateFile)

	// Second maintenance (no changes) should not rewrite
	s.Maintain(acct)

	info2, _ := os.Stat(dateFile)
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

// --- Thread interleaving ---

func TestInterleaveThreads_RepliesAfterParent(t *testing.T) {
	s, acct := setup(t)

	// Write parent to date file
	parent := msgLine("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread start")
	s.Append(acct, "#general", parent)

	// Write another message after parent
	m2 := msgLine("M2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "unrelated")
	s.Append(acct, "#general", m2)

	// Write parent + reply to thread file
	s.AppendThread(acct, "#general", "P1", parent)
	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0),
			Sender: "Bob", SenderID: "U2", Text: "reply here", Reply: true,
		},
	}
	s.AppendThread(acct, "#general", "P1", reply)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// Should be: parent, reply (interleaved), m2
	if len(df.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(df.Messages))
	}
	if df.Messages[0].ID != "P1" {
		t.Errorf("messages[0] = %q, want P1", df.Messages[0].ID)
	}
	if df.Messages[1].ID != "R1" || !df.Messages[1].Reply {
		t.Errorf("messages[1] = %q reply=%v, want R1 reply=true", df.Messages[1].ID, df.Messages[1].Reply)
	}
	if df.Messages[2].ID != "M2" {
		t.Errorf("messages[2] = %q, want M2", df.Messages[2].ID)
	}
}

func TestInterleaveThreads_NoThreadDir(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	s.Append(acct, "#general", m1)

	// No thread files — should return messages unchanged, no error
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(df.Messages))
	}
}

func TestInterleaveThreads_CorruptThreadFile(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	s.Append(acct, "#general", m1)

	// Create a corrupt thread file
	conv := s.convDir(acct, "#general")
	os.MkdirAll(conv.ThreadsDir(), 0755)
	os.WriteFile(conv.ThreadFile("CORRUPT"), []byte("not valid jsonl\n"), 0644)

	// Should return messages (partial data). Corrupt thread file parses with
	// skipped lines but has no valid parent, so it's not interleaved.
	df, _ := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if df == nil {
		t.Fatal("expected partial data, got nil")
	}
	if len(df.Messages) != 1 {
		t.Errorf("messages = %d, want 1 (original message preserved)", len(df.Messages))
	}
}

func TestInterleaveThreads_MultipleThreads(t *testing.T) {
	s, acct := setup(t)

	p1 := msgLine("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "first thread")
	p2 := msgLine("P2", ts(2026, 3, 16, 9, 5, 0), "Bob", "U2", "second thread")
	s.Append(acct, "#general", p1)
	s.Append(acct, "#general", p2)

	// Thread 1
	s.AppendThread(acct, "#general", "P1", p1)
	r1 := modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
		ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply to first", Reply: true,
	}}
	s.AppendThread(acct, "#general", "P1", r1)

	// Thread 2
	s.AppendThread(acct, "#general", "P2", p2)
	r2 := modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
		ID: "R2", Ts: ts(2026, 3, 16, 9, 6, 0), Sender: "Alice", SenderID: "U1", Text: "reply to second", Reply: true,
	}}
	s.AppendThread(acct, "#general", "P2", r2)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// P1, R1, P2, R2
	if len(df.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(df.Messages))
	}
	ids := make([]string, len(df.Messages))
	for i, m := range df.Messages {
		ids[i] = m.ID
	}
	want := []string{"P1", "R1", "P2", "R2"}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("messages[%d] = %q, want %q (order: %v)", i, id, want[i], ids)
			break
		}
	}
}

func TestInterleaveThreads_ThreadWithReactions(t *testing.T) {
	s, acct := setup(t)

	p1 := msgLine("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread start")
	s.Append(acct, "#general", p1)

	// Thread with reply + reaction
	s.AppendThread(acct, "#general", "P1", p1)
	reply := modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
		ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "nice", Reply: true,
	}}
	s.AppendThread(acct, "#general", "P1", reply)
	react := modelv1.Line{Type: modelv1.LineReaction, React: &modelv1.ReactLine{
		Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "P1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup",
	}}
	// Reaction goes to both date file and thread file (per protocol).
	s.Append(acct, "#general", react)
	s.AppendThread(acct, "#general", "P1", react)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// Parent + reply interleaved
	if len(df.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(df.Messages))
	}
	// Parent should have the reaction from the date file
	if len(df.Messages[0].Reactions) != 1 {
		t.Errorf("parent reactions = %d, want 1", len(df.Messages[0].Reactions))
	}
}
