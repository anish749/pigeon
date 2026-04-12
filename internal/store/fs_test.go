package store

import (
	"fmt"
	"os"
	"path/filepath"
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
	data, err := os.ReadFile(conv.DateFile("2026-03-16").Path())
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
	if _, err := os.Stat(stateFile); err != nil {
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
	info1, _ := os.Stat(dateFile.Path())

	// Second maintenance (no changes) should not rewrite
	s.Maintain(acct)

	info2, _ := os.Stat(dateFile.Path())
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
	os.WriteFile(conv.ThreadFile("CORRUPT").Path(), []byte("not valid jsonl\n"), 0644)

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

// --- ReadConversation options ---

// setupMultiDay populates a conversation with messages across 5 days
// (March 12-16, 2026) with 3 messages per day (at 09:00, 12:00, 18:00 UTC).
// Returns message IDs in chronological order.
func setupMultiDay(t *testing.T, s *FSStore, acct account.Account) []string {
	t.Helper()
	var ids []string
	for day := 12; day <= 16; day++ {
		for _, hour := range []int{9, 12, 18} {
			id := fmt.Sprintf("D%d-H%d", day, hour)
			ids = append(ids, id)
			m := msgLine(id, ts(2026, 3, day, hour, 0, 0), "Alice", "U1", fmt.Sprintf("msg day %d hour %d", day, hour))
			if err := s.Append(acct, "#general", m); err != nil {
				t.Fatalf("Append %s: %v", id, err)
			}
		}
	}
	return ids
}

func TestReadConversation_Last(t *testing.T) {
	s, acct := setup(t)
	ids := setupMultiDay(t, s, acct)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Last: 5})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 5 {
		t.Fatalf("messages = %d, want 5", len(df.Messages))
	}
	// Should be the last 5 messages chronologically
	want := ids[len(ids)-5:]
	for i, m := range df.Messages {
		if m.ID != want[i] {
			t.Errorf("messages[%d] = %q, want %q", i, m.ID, want[i])
		}
	}
}

func TestReadConversation_Last_MoreThanAvailable(t *testing.T) {
	s, acct := setup(t)
	setupMultiDay(t, s, acct) // 15 messages total

	df, err := s.ReadConversation(acct, "#general", ReadOpts{Last: 100})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 15 {
		t.Errorf("messages = %d, want 15 (all available)", len(df.Messages))
	}
}

func TestReadConversation_Since(t *testing.T) {
	s, acct := setup(t)
	setupMultiDay(t, s, acct)

	// Since 2 days: cutoff is March 14 at current time.
	// File selection picks files >= March 14 (dates 14, 15, 16 = 9 messages).
	// Precise filter then removes messages before the exact cutoff.
	// Because time.Now() varies, just verify we get messages from the right date files.
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Since: 48 * time.Hour})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// All returned messages should be within the last 48h
	cutoff := time.Now().Add(-48 * time.Hour)
	for _, m := range df.Messages {
		if m.Ts.Before(cutoff) {
			t.Errorf("message %s at %v is before cutoff %v", m.ID, m.Ts, cutoff)
		}
	}
}

func TestReadConversation_Since_SelectsCorrectFiles(t *testing.T) {
	s, acct := setup(t)

	// Write messages across 3 days relative to now
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for dayOffset := -2; dayOffset <= 0; dayOffset++ {
		d := today.Add(time.Duration(dayOffset) * 24 * time.Hour)
		for _, hour := range []int{9, 12, 18} {
			t2 := d.Add(time.Duration(hour) * time.Hour)
			id := fmt.Sprintf("D%d-H%d", dayOffset, hour)
			m := msgLine(id, t2, "Alice", "U1", "msg")
			if err := s.Append(acct, "#general", m); err != nil {
				t.Fatalf("Append %s: %v", id, err)
			}
		}
	}

	// Since 3 days should include all files
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Since: 3 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) < 6 {
		t.Errorf("messages = %d, want at least 6", len(df.Messages))
	}
}

func TestReadConversation_SinceAndLast(t *testing.T) {
	s, acct := setup(t)

	// Write messages across 3 days relative to now
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for dayOffset := -2; dayOffset <= 0; dayOffset++ {
		d := today.Add(time.Duration(dayOffset) * 24 * time.Hour)
		for _, hour := range []int{9, 12, 18} {
			t2 := d.Add(time.Duration(hour) * time.Hour)
			id := fmt.Sprintf("D%d-H%d", dayOffset, hour)
			m := msgLine(id, t2, "Alice", "U1", "msg")
			if err := s.Append(acct, "#general", m); err != nil {
				t.Fatalf("Append %s: %v", id, err)
			}
		}
	}

	// Since 3 days gets all messages, then last 3 caps it
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Since: 3 * 24 * time.Hour, Last: 3})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(df.Messages))
	}
	for i := 1; i < len(df.Messages); i++ {
		if df.Messages[i].Ts.Before(df.Messages[i-1].Ts) {
			t.Errorf("messages not in chronological order: %v before %v", df.Messages[i].Ts, df.Messages[i-1].Ts)
		}
	}
}

func TestReadConversation_Default_Last25(t *testing.T) {
	s, acct := setup(t)

	// Write 30 messages across multiple days
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		d := today.Add(-time.Duration(29-i) * time.Hour)
		id := fmt.Sprintf("M%02d", i)
		m := msgLine(id, d, "Alice", "U1", "msg")
		if err := s.Append(acct, "#general", m); err != nil {
			t.Fatalf("Append %s: %v", id, err)
		}
	}

	df, err := s.ReadConversation(acct, "#general", ReadOpts{})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	// Default should return last 25 messages
	if len(df.Messages) != 25 {
		t.Fatalf("messages = %d, want 25", len(df.Messages))
	}
	// First returned message should be M05 (skipped M00-M04)
	if df.Messages[0].ID != "M05" {
		t.Errorf("first message = %q, want M05", df.Messages[0].ID)
	}
	if df.Messages[24].ID != "M29" {
		t.Errorf("last message = %q, want M29", df.Messages[24].ID)
	}
}

func TestReadConversation_Default_LessThan25(t *testing.T) {
	s, acct := setup(t)

	// Write only 3 messages — should return all of them
	m1 := msgLine("A", ts(2026, 1, 10, 9, 0, 0), "Alice", "U1", "first")
	m2 := msgLine("B", ts(2026, 1, 10, 12, 0, 0), "Bob", "U2", "second")
	m3 := msgLine("C", ts(2026, 1, 11, 9, 0, 0), "Alice", "U1", "third")
	s.Append(acct, "#general", m1)
	s.Append(acct, "#general", m2)
	s.Append(acct, "#general", m3)

	df, err := s.ReadConversation(acct, "#general", ReadOpts{})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (all available)", len(df.Messages))
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

// TestMaintain_ConversationNamedThreads verifies that a conversation
// literally named "threads" has its date files compacted via Compact
// (not CompactThread). The date file for such a conversation lives
// at <acct>/threads/YYYY-MM-DD.jsonl, which shares its parent-dir
// heuristic with real thread files.
//
// The distinguishing setup: a message and a later delete for that
// message. CompactThread would treat the message as the thread parent,
// see it deleted, and return nil — triggering os.Remove on the date
// file and losing data. Compact reconciles the delete into removing
// the message from the file, but the file itself remains on disk.
func TestMaintain_ConversationNamedThreads(t *testing.T) {
	s, acct := setup(t)

	m1 := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	m2 := msgLine("M2", ts(2026, 3, 16, 9, 1, 0), "Bob", "U2", "still here")
	del := modelv1.Line{
		Type: modelv1.LineDelete,
		Delete: &modelv1.DeleteLine{
			Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1",
			Sender: "Alice", SenderID: "U1",
		},
	}

	for _, line := range []modelv1.Line{m1, m2, del} {
		if err := s.Append(acct, "threads", line); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	// With the bug, CompactThread treats M1 as the thread parent, sees
	// the delete, returns nil, and maintainFile calls os.Remove — so
	// the entire date file (including M2) is lost. With the fix, Compact
	// removes only M1 and the file is rewritten with M2 intact.
	conv := s.convDir(acct, "threads")
	datePath := conv.DateFile("2026-03-16").Path()
	data, err := os.ReadFile(datePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", datePath, err)
	}
	df, parseErr := modelv1.ParseDateFile(data)
	if parseErr != nil {
		t.Fatalf("ParseDateFile after maintain: %v", parseErr)
	}
	if len(df.Messages) != 1 || df.Messages[0].ID != "M2" {
		t.Errorf("after maintenance: messages = %+v, want [M2]", df.Messages)
	}
}

func TestRemoveDriveFile_SluggedDir(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.Platform("gws").AccountFromSlug("test").Drive()

	target := filepath.Join(driveDir.Path(), "my-doc-fileID123")
	keep := filepath.Join(driveDir.Path(), "other-doc-fileID456")
	for _, d := range []string{target, keep} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "content.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RemoveDriveFile(driveDir, "fileID123"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target dir still exists: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated dir removed: %v", err)
	}
}

func TestRemoveDriveFile_PlainIDDir(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.Platform("gws").AccountFromSlug("test").Drive()
	target := filepath.Join(driveDir.Path(), "fileID789")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDriveFile(driveDir, "fileID789"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target dir still exists: %v", err)
	}
}

func TestRemoveDriveFile_NoMatch(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.Platform("gws").AccountFromSlug("test").Drive()
	keep := filepath.Join(driveDir.Path(), "unrelated-doc-fileIDAAA")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDriveFile(driveDir, "fileIDZZZ"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("unrelated dir removed: %v", err)
	}
}

func TestRemoveDriveFile_IgnoresNonDirectories(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.Platform("gws").AccountFromSlug("test").Drive()
	if err := os.MkdirAll(driveDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(driveDir.Path(), "stray-fileID123")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveDriveFile(driveDir, "fileID123"); err != nil {
		t.Fatalf("RemoveDriveFile: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray file incorrectly removed: %v", err)
	}
}

func TestRemoveDriveFile_MissingDriveDir(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.Platform("gws").AccountFromSlug("neverbackfilled").Drive()

	if err := s.RemoveDriveFile(driveDir, "fileIDXYZ"); err != nil {
		t.Errorf("RemoveDriveFile on missing dir: %v", err)
	}
}

// --- GWS persistence method tests ---

func gwsEmailLine(id string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID:      id,
			Subject: "Subject " + id,
			Ts:      ts(2026, 4, 7, 12, 0, 0),
			From:    "test@example.com",
			To:      []string{"to@example.com"},
			Labels:  []string{"INBOX"},
		},
	}
}

func TestAppendLineAndReadLines(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "test.jsonl")

	for _, id := range []string{"a", "b", "c"} {
		if err := s.AppendLine(paths.DateFile(path), gwsEmailLine(id)); err != nil {
			t.Fatalf("AppendLine(%q): %v", id, err)
		}
	}

	lines, err := s.ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, want := range []string{"a", "b", "c"} {
		if lines[i].Email.ID != want {
			t.Errorf("lines[%d].ID = %q, want %q", i, lines[i].Email.ID, want)
		}
	}
}

func TestWriteLines_Overwrite(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "write.jsonl")

	initial := []modelv1.Line{gwsEmailLine("a"), gwsEmailLine("b")}
	if err := s.WriteLines(paths.DateFile(path), initial); err != nil {
		t.Fatalf("WriteLines: %v", err)
	}

	lines, err := s.ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	// Overwrite with fewer lines — verifies replacement, not append.
	if err := s.WriteLines(paths.DateFile(path), []modelv1.Line{gwsEmailLine("c")}); err != nil {
		t.Fatalf("WriteLines overwrite: %v", err)
	}
	lines, err = s.ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines after overwrite: %v", err)
	}
	if len(lines) != 1 || lines[0].Email.ID != "c" {
		t.Fatalf("got %v, want 1 line with ID=c", lines)
	}
}

func TestWriteLines_Empty(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "empty.jsonl")

	if err := s.WriteLines(paths.DateFile(path), []modelv1.Line{gwsEmailLine("a")}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteLines(paths.DateFile(path), nil); err != nil {
		t.Fatalf("WriteLines empty: %v", err)
	}
	lines, err := s.ReadLines(paths.DateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %d lines, want 0", len(lines))
	}
}

func TestReadLines_NonExistent(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	lines, err := s.ReadLines(paths.DateFile(filepath.Join(root.Path(), "nope.jsonl")))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if lines != nil {
		t.Fatalf("got %v, want nil", lines)
	}
}

func TestReadLines_CorruptLines(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "corrupt.jsonl")

	if err := s.AppendLine(paths.DateFile(path), gwsEmailLine("good1")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not valid json\n")
	f.Close()
	if err := s.AppendLine(paths.DateFile(path), gwsEmailLine("good2")); err != nil {
		t.Fatal(err)
	}

	lines, err := s.ReadLines(paths.DateFile(path))
	if err == nil {
		t.Fatal("expected error for corrupt line")
	}
	if len(lines) != 2 {
		t.Fatalf("got %d good lines, want 2", len(lines))
	}
	if lines[0].Email.ID != "good1" || lines[1].Email.ID != "good2" {
		t.Errorf("lines = %v, want [good1, good2]", lines)
	}
}

func TestWriteContent_RoundTrip(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "output.md")

	content := []byte("# Hello\nWorld\n")
	if err := s.WriteContent(paths.TabFile(path), content); err != nil {
		t.Fatalf("WriteContent: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestWriteContent_Replaces(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "output.md")

	if err := s.WriteContent(paths.TabFile(path), []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteContent(paths.TabFile(path), []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestWriteContent_CreatesParentDirs(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	path := filepath.Join(root.Path(), "a", "b", "c", "output.csv")

	content := []byte("col1,col2\n1,2\n")
	if err := s.WriteContent(paths.TabFile(path), content); err != nil {
		t.Fatalf("WriteContent: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func testGWSDriveFileDir(t *testing.T, s *FSStore) paths.DriveFileDir {
	t.Helper()
	return paths.NewDataRoot(t.TempDir()).
		Platform("gws").
		AccountFromSlug("test").
		Drive().
		File("doc-abc")
}

func TestDriveMetaRoundTrip(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	fileDir := testGWSDriveFileDir(t, s)
	mf := fileDir.MetaFile("2026-04-07")

	orig := &modelv1.DocMeta{
		FileID:       "file-123",
		MimeType:     "application/vnd.google-apps.document",
		Title:        "My Doc",
		ModifiedTime: "2026-04-07T12:00:00Z",
		SyncedAt:     "2026-04-07T12:01:00Z",
		Tabs: []modelv1.TabMeta{
			{ID: "tab-1", Title: "Main"},
			{ID: "tab-2", Title: "Notes"},
		},
		Sheets: []string{"Sheet1", "Sheet2"},
	}

	if err := s.SaveDriveMeta(mf, orig); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}
	got, err := s.LoadDriveMeta(mf)
	if err != nil {
		t.Fatalf("LoadDriveMeta: %v", err)
	}
	if got.FileID != orig.FileID || got.Title != orig.Title {
		t.Errorf("round trip mismatch: got %+v", got)
	}
	if len(got.Tabs) != 2 || len(got.Sheets) != 2 {
		t.Errorf("Tabs=%d Sheets=%d, want 2/2", len(got.Tabs), len(got.Sheets))
	}
}

func TestLoadDriveMetaNonExistent(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	mf := testGWSDriveFileDir(t, s).MetaFile("2026-04-07")
	_, err := s.LoadDriveMeta(mf)
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestSaveDriveMetaCleansUpStaleFiles(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	fileDir := testGWSDriveFileDir(t, s)

	if err := os.MkdirAll(fileDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(fileDir.Path(), "drive-meta-2026-04-01.json")
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mf := fileDir.MetaFile("2026-04-07")
	if err := s.SaveDriveMeta(mf, &modelv1.DocMeta{FileID: "f1"}); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}

	if _, err := os.Stat(mf.Path()); err != nil {
		t.Errorf("new meta file missing: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("stale meta file not cleaned up: err=%v", err)
	}
}

func TestSaveDriveMetaLeavesUnrelatedFiles(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	fileDir := testGWSDriveFileDir(t, s)
	if err := os.MkdirAll(fileDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}

	unrelated := []string{
		filepath.Join(fileDir.Path(), "Tab1.md"),
		filepath.Join(fileDir.Path(), "comments.jsonl"),
	}
	for _, p := range unrelated {
		if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mf := fileDir.MetaFile("2026-04-07")
	if err := s.SaveDriveMeta(mf, &modelv1.DocMeta{FileID: "f1"}); err != nil {
		t.Fatalf("SaveDriveMeta: %v", err)
	}
	for _, p := range unrelated {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("unrelated file %s was removed: %v", p, err)
		}
	}
}

func TestGWSCursorsRoundTrip(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := root.Platform("gws").AccountFromSlug("test")

	orig := &GWSCursors{
		Gmail: GmailCursors{HistoryID: "12345"},
		Drive: GWSDriveCursors{PageToken: "tok-abc"},
		Calendar: GWSCalendarCursors{
			"primary": {
				SyncToken:       "sync-1",
				ExpandedUntil:   "2026-07-07T00:00:00Z",
				RecurringEvents: []string{"evt-a", "evt-b"},
			},
			"work@test.com": {SyncToken: "sync-2"},
		},
	}

	if err := s.SaveGWSCursors(acct, orig); err != nil {
		t.Fatalf("SaveGWSCursors: %v", err)
	}
	got, err := s.LoadGWSCursors(acct)
	if err != nil {
		t.Fatalf("LoadGWSCursors: %v", err)
	}
	if got.Gmail.HistoryID != "12345" {
		t.Errorf("Gmail.HistoryID = %q, want 12345", got.Gmail.HistoryID)
	}
	if got.Drive.PageToken != "tok-abc" {
		t.Errorf("Drive.PageToken = %q, want tok-abc", got.Drive.PageToken)
	}
	if len(got.Calendar) != 2 {
		t.Fatalf("Calendar has %d entries, want 2", len(got.Calendar))
	}
	primary := got.Calendar["primary"]
	if primary == nil || primary.SyncToken != "sync-1" {
		t.Errorf("Calendar[primary] = %+v, want sync-1", primary)
	}
	if len(primary.RecurringEvents) != 2 {
		t.Errorf("RecurringEvents = %d, want 2", len(primary.RecurringEvents))
	}
}

func TestLoadGWSCursors_NonExistent(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := root.Platform("gws").AccountFromSlug("neversynced")

	got, err := s.LoadGWSCursors(acct)
	if err != nil {
		t.Fatalf("LoadGWSCursors: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil GWSCursors")
	}
	if got.Gmail.HistoryID != "" {
		t.Errorf("Gmail.HistoryID = %q, want empty", got.Gmail.HistoryID)
	}
}

func TestAppendPendingDelete(t *testing.T) {
	s, _ := setup(t)
	acct := paths.NewDataRoot(t.TempDir()).Platform("gws").AccountFromSlug("test")
	gmailDir := acct.Gmail()

	for _, id := range []string{"msg-1", "msg-2", "msg-3"} {
		if err := s.AppendPendingDelete(gmailDir, id); err != nil {
			t.Fatalf("AppendPendingDelete(%s): %v", id, err)
		}
	}

	data, err := os.ReadFile(gmailDir.PendingDeletesPath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "msg-1\nmsg-2\nmsg-3\n"
	if string(data) != want {
		t.Errorf("pending deletes = %q, want %q", data, want)
	}
}

// --- GWS JSONL maintenance ---

func emailLine(id string, t time.Time, subject string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID:      id,
			Ts:      t,
			Subject: subject,
			To:      []string{"user@example.com"},
			Labels:  []string{"INBOX"},
		},
	}
}

// TestMaintain_GmailDedup verifies that Gmail daily files are deduplicated
// by ID with the last occurrence winning (an email re-synced after a label
// change overwrites the earlier copy).
func TestMaintain_GmailDedup(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	gmail := root.AccountFor(acct).Gmail()

	date := "2026-03-16"
	df := gmail.DateFile(date)

	// Two writes of the same email ID — the second ("updated") wins.
	for _, l := range []modelv1.Line{
		emailLine("E1", ts(2026, 3, 16, 9, 0, 0), "hello"),
		emailLine("E2", ts(2026, 3, 16, 10, 0, 0), "world"),
		emailLine("E1", ts(2026, 3, 16, 9, 0, 0), "updated"),
	} {
		if err := s.AppendLine(df, l); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	data, err := os.ReadFile(df.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines, perr := parseGWSLines(data)
	if perr != nil {
		t.Fatalf("parseGWSLines: %v", perr)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	// DedupGWS keeps the last occurrence in input order. E2 was between
	// the two E1 entries, so the surviving order is [E2, E1-updated].
	if lines[0].Email.ID != "E2" || lines[1].Email.ID != "E1" {
		t.Errorf("order = [%s, %s], want [E2, E1]", lines[0].Email.ID, lines[1].Email.ID)
	}
	if got := lines[1].Email.Subject; got != "updated" {
		t.Errorf("E1 subject = %q, want %q (last-write wins)", got, "updated")
	}
}

// TestMaintain_CalendarDedup verifies that Calendar daily files are
// deduplicated by event ID.
func TestMaintain_CalendarDedup(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	cal := root.AccountFor(acct).Calendar("primary")

	df := cal.DateFile("2026-03-16")

	raw := func(id, summary string) modelv1.Line {
		serialized := map[string]any{
			"id":      id,
			"summary": summary,
		}
		return modelv1.Line{
			Type: modelv1.LineEvent,
			Event: &modelv1.CalendarEvent{
				Serialized: serialized,
			},
		}
	}
	// Populate the Runtime.Id field via the serialised-then-parsed path
	// so that Line.ID() returns the expected ID. Easier: just re-parse
	// the marshalled bytes.
	marshal := func(l modelv1.Line) modelv1.Line {
		b, err := modelv1.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		parsed, err := modelv1.Parse(string(b))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return parsed
	}

	for _, l := range []modelv1.Line{
		marshal(raw("EV1", "standup")),
		marshal(raw("EV2", "1:1")),
		marshal(raw("EV1", "standup (rescheduled)")),
	} {
		if err := s.AppendLine(df, l); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	data, err := os.ReadFile(df.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines, perr := parseGWSLines(data)
	if perr != nil {
		t.Fatalf("parseGWSLines: %v", perr)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	id0, _ := lines[0].ID()
	id1, _ := lines[1].ID()
	if id0 != "EV2" || id1 != "EV1" {
		t.Errorf("ids = [%s, %s], want [EV2, EV1]", id0, id1)
	}
	if got := lines[1].Event.Runtime.Summary; got != "standup (rescheduled)" {
		t.Errorf("EV1 summary = %q, want last-write value", got)
	}
}

// TestMaintain_PendingEmailDeletes verifies that Gmail maintenance applies
// entries from .pending-email-deletes, filters out the matching email lines,
// and removes the pending-deletes file after successful processing.
func TestMaintain_PendingEmailDeletes(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	gmail := root.AccountFor(acct).Gmail()

	// Populate two days of Gmail data: E1 and E3 span two files; E2 and E4
	// stay on their respective days. Pending deletes target E1 and E4.
	day1 := gmail.DateFile("2026-03-15")
	day2 := gmail.DateFile("2026-03-16")
	for _, pair := range []struct {
		df paths.DateFile
		l  modelv1.Line
	}{
		{day1, emailLine("E1", ts(2026, 3, 15, 9, 0, 0), "keep?")},
		{day1, emailLine("E2", ts(2026, 3, 15, 9, 1, 0), "stays")},
		{day2, emailLine("E3", ts(2026, 3, 16, 9, 0, 0), "stays")},
		{day2, emailLine("E4", ts(2026, 3, 16, 9, 1, 0), "delete me")},
	} {
		if err := s.AppendLine(pair.df, pair.l); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	for _, id := range []string{"E1", "E4"} {
		if err := s.AppendPendingDelete(gmail, id); err != nil {
			t.Fatalf("AppendPendingDelete(%s): %v", id, err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	for _, tc := range []struct {
		path string
		want []string
	}{
		{day1.Path(), []string{"E2"}},
		{day2.Path(), []string{"E3"}},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", tc.path, err)
		}
		lines, perr := parseGWSLines(data)
		if perr != nil {
			t.Fatalf("parseGWSLines %s: %v", tc.path, perr)
		}
		got := make([]string, len(lines))
		for i, l := range lines {
			got[i] = l.Email.ID
		}
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tc.want) {
			t.Errorf("%s: ids = %v, want %v", tc.path, got, tc.want)
		}
	}

	// Pending deletes file should be gone after successful application.
	if _, err := os.Stat(gmail.PendingDeletesPath()); !os.IsNotExist(err) {
		t.Errorf("pending deletes file still present: err = %v", err)
	}
}

// TestMaintain_PendingEmailDeletes_ForcesUnchangedFiles verifies that the
// mtime-based skip does not apply when there are pending deletes — even a
// Gmail file that hasn't been appended to since the previous maintenance
// gets the delete applied.
func TestMaintain_PendingEmailDeletes_ForcesUnchangedFiles(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	gmail := root.AccountFor(acct).Gmail()
	df := gmail.DateFile("2026-03-15")

	for _, l := range []modelv1.Line{
		emailLine("E1", ts(2026, 3, 15, 9, 0, 0), "a"),
		emailLine("E2", ts(2026, 3, 15, 9, 1, 0), "b"),
	} {
		if err := s.AppendLine(df, l); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	// First maintenance records current mtime.
	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	// Queue a delete and re-run without touching the file.
	if err := s.AppendPendingDelete(gmail, "E1"); err != nil {
		t.Fatalf("AppendPendingDelete: %v", err)
	}
	if err := s.Maintain(acct); err != nil {
		t.Fatalf("second Maintain: %v", err)
	}

	data, err := os.ReadFile(df.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines, perr := parseGWSLines(data)
	if perr != nil {
		t.Fatalf("parseGWSLines: %v", perr)
	}
	if len(lines) != 1 || lines[0].Email.ID != "E2" {
		t.Errorf("lines after delete = %+v, want [E2]", lines)
	}
	if _, err := os.Stat(gmail.PendingDeletesPath()); !os.IsNotExist(err) {
		t.Errorf("pending deletes file still present")
	}
}

// TestMaintain_SkipsDotfiles ensures the walker leaves hidden JSONL files
// (e.g. .poll-metrics.jsonl) untouched — they have an unrelated schema
// and were previously parsed as empty date files and truncated to zero.
func TestMaintain_SkipsDotfiles(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	ad := root.AccountFor(acct)

	if err := os.MkdirAll(ad.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	metrics := ad.PollMetricsPath()
	contents := []byte(`{"service":"gmail","startedAt":"2026-03-16T00:00:00Z"}` + "\n")
	if err := os.WriteFile(metrics, contents, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	got, err := os.ReadFile(metrics)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(contents) {
		t.Errorf("poll metrics mutated by Maintain: got %q, want %q", got, contents)
	}
}
