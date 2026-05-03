package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	stateFile := s.root.AccountFor(acct).MaintenanceFile()
	if _, err := os.Stat(stateFile.Path()); err != nil {
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

// Regression: a date file with unparseable lines must not be compacted —
// the file is left untouched to prevent silently dropping data.
func TestMaintain_SkipsDateFileWithParseError(t *testing.T) {
	s, acct := setup(t)
	conv := s.convDir(acct, "#general")
	datePath := conv.DateFile("2026-03-16").Path()

	// Write one valid line followed by garbage.
	if err := s.Append(acct, "#general", msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	original, _ := os.ReadFile(datePath)
	os.WriteFile(datePath, append(original, []byte("this is garbage\n")...), 0644)
	original = append(original, []byte("this is garbage\n")...)

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	got, _ := os.ReadFile(datePath)
	if string(got) != string(original) {
		t.Errorf("date file was modified despite parse error\ngot:  %q\nwant: %q", got, original)
	}
}

// Regression: a thread file with unparseable lines must not be compacted.
func TestMaintain_SkipsThreadFileWithParseError(t *testing.T) {
	s, acct := setup(t)
	conv := s.convDir(acct, "#general")

	// Seed a valid thread file, then corrupt it.
	parent := msgLine("P1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "thread start")
	if err := s.AppendThread(acct, "#general", "P1", parent); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}
	threadPath := string(conv.ThreadFile("P1"))
	original, _ := os.ReadFile(threadPath)
	corrupted := append(original, []byte("this is garbage\n")...)
	os.WriteFile(threadPath, corrupted, 0644)

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	got, _ := os.ReadFile(threadPath)
	if string(got) != string(corrupted) {
		t.Errorf("thread file was modified despite parse error\ngot:  %q\nwant: %q", got, corrupted)
	}
}

// Regression: if compaction would produce an empty file, the original must
// be left untouched.
func TestMaintain_SkipsFileIfCompactionWouldEmpty(t *testing.T) {
	s, acct := setup(t)
	m := msgLine("M1", ts(2026, 3, 16, 9, 0, 0), "Alice", "U1", "hello")
	del := modelv1.Line{
		Type:   modelv1.LineDelete,
		Delete: &modelv1.DeleteLine{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1"},
	}
	if err := s.Append(acct, "#general", m); err != nil {
		t.Fatalf("Append msg: %v", err)
	}
	if err := s.Append(acct, "#general", del); err != nil {
		t.Fatalf("Append delete: %v", err)
	}

	datePath := s.convDir(acct, "#general").DateFile("2026-03-16").Path()
	original, _ := os.ReadFile(datePath)

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	got, _ := os.ReadFile(datePath)
	if string(got) != string(original) {
		t.Errorf("file was modified despite compaction emptying it\ngot:  %q\nwant: %q", got, original)
	}
}

// Regression: files under the identity/ subdirectory must not be compacted.
func TestMaintain_SkipsIdentityFiles(t *testing.T) {
	s, acct := setup(t)
	identityPath := string(s.root.AccountFor(acct).Identity().PeopleFile())
	original := []byte(`{"type":"person","id":"U1","name":"Alice"}` + "\n")
	os.MkdirAll(filepath.Dir(identityPath), 0755)
	os.WriteFile(identityPath, original, 0644)

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	got, _ := os.ReadFile(identityPath)
	if string(got) != string(original) {
		t.Errorf("identity file was modified during maintenance")
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

func TestMaintain_GWSDedup(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user-at-gmail-com")
	gmailDir := root.AccountFor(acct).Gmail()
	dateFile := gmailDir.DateFile("2026-04-07")

	// Write three email lines, two with the same ID (simulating a re-sync).
	e1 := modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID: "email1", Subject: "First", Ts: ts(2026, 4, 7, 10, 0, 0),
			From: "a@example.com", To: []string{"b@example.com"}, Labels: []string{"INBOX"},
		},
	}
	e2old := modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID: "email2", Subject: "Old subject", Ts: ts(2026, 4, 7, 11, 0, 0),
			From: "a@example.com", To: []string{"b@example.com"}, Labels: []string{"INBOX"},
		},
	}
	e2new := modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID: "email2", Subject: "Updated subject", Ts: ts(2026, 4, 7, 11, 0, 0),
			From: "a@example.com", To: []string{"b@example.com"}, Labels: []string{"INBOX", "STARRED"},
		},
	}

	for _, line := range []modelv1.Line{e1, e2old, e2new} {
		if err := s.AppendLine(dateFile, line); err != nil {
			t.Fatalf("AppendLine: %v", err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	lines, err := s.ReadLines(dateFile)
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("after maintenance: got %d lines, want 2 (deduped)", len(lines))
	}
	if lines[0].Email.ID != "email1" {
		t.Errorf("lines[0].ID = %q, want %q", lines[0].Email.ID, "email1")
	}
	if lines[1].Email.ID != "email2" {
		t.Errorf("lines[1].ID = %q, want %q", lines[1].Email.ID, "email2")
	}
	if lines[1].Email.Subject != "Updated subject" {
		t.Errorf("lines[1].Subject = %q, want %q (last occurrence kept)", lines[1].Email.Subject, "Updated subject")
	}
}

// TestMaintainFile_FailsOnUnknownKind exercises the default arm of
// maintainFile's type switch. paths.Classify returns nil for any .jsonl
// path that doesn't match a known shape; the dispatch must fail loud
// rather than silently miscompacting. This is the regression guard that
// catches a future paths kind landing without an explicit case.
func TestMaintainFile_FailsOnUnknownKind(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)

	// A .jsonl path that does not match any shape paths.Classify knows
	// about — Classify returns nil, dispatch hits default.
	stray := filepath.Join(t.TempDir(), "stray.jsonl")
	if err := os.WriteFile(stray, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if paths.Classify(stray) != nil {
		t.Fatalf("test setup: expected Classify(%q) == nil", stray)
	}

	err := s.maintainFile(stray)
	if err == nil {
		t.Fatal("maintainFile returned nil for an unclassified .jsonl, want error")
	}
	if !strings.Contains(err.Error(), "unhandled DataFile kind") {
		t.Errorf("error = %v, want it to mention 'unhandled DataFile kind'", err)
	}
}

// TestMaintain_LinearDedup verifies that the Classify-based dispatch in
// maintainFile routes Linear's split logs (issue.jsonl, comments.jsonl)
// through DedupGWS — the same id-based dedup the rest of the GWS family
// uses. Regression guard for the IsGWSFile → Classify refactor: if a
// future paths kind lands without an explicit case, the maintainFile
// type-switch fails loud rather than silently miscompacting.
func TestMaintain_LinearDedup(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("linear", "my-team")
	issueDir := root.AccountFor(acct).Linear().Issue("ENG-101")

	issue := func(id, title, updatedAt string) modelv1.Line {
		return modelv1.Line{
			Type: modelv1.LineLinearIssue,
			Issue: &modelv1.LinearIssue{
				Runtime: modelv1.LinearIssueRuntime{
					ID: id, Identifier: "ENG-101", UpdatedAt: updatedAt,
				},
				Serialized: map[string]any{
					"id": id, "identifier": "ENG-101", "title": title, "updatedAt": updatedAt,
				},
			},
		}
	}
	comment := func(id, body, createdAt string) modelv1.Line {
		return modelv1.Line{
			Type: modelv1.LineLinearComment,
			LinearComment: &modelv1.LinearComment{
				Runtime: modelv1.LinearCommentRuntime{ID: id, CreatedAt: createdAt},
				Serialized: map[string]any{
					"id": id, "body": body, "createdAt": createdAt,
				},
			},
		}
	}

	// Two snapshots of the same issue (one stale, one current) and two
	// fetches of the same comment (the second carries a body edit). After
	// maintenance each file should keep only the last occurrence of each id.
	for _, line := range []modelv1.Line{
		issue("uuid-1", "Old title", "2026-04-01T10:00:00Z"),
		issue("uuid-1", "New title", "2026-04-02T10:00:00Z"),
	} {
		if err := s.AppendLine(issueDir.IssueFile(), line); err != nil {
			t.Fatalf("AppendLine issue: %v", err)
		}
	}
	for _, line := range []modelv1.Line{
		comment("c1", "first draft", "2026-04-01T11:00:00Z"),
		comment("c1", "edited", "2026-04-01T11:00:00Z"),
	} {
		if err := s.AppendLine(issueDir.CommentsFile(), line); err != nil {
			t.Fatalf("AppendLine comment: %v", err)
		}
	}

	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	issueLines, err := s.ReadLines(issueDir.IssueFile())
	if err != nil {
		t.Fatalf("ReadLines issue.jsonl: %v", err)
	}
	if len(issueLines) != 1 {
		t.Fatalf("issue.jsonl: got %d lines, want 1 (deduped)", len(issueLines))
	}
	if got := issueLines[0].Issue.Serialized["title"]; got != "New title" {
		t.Errorf("issue.jsonl: title = %v, want %q (last occurrence kept)", got, "New title")
	}

	commentLines, err := s.ReadLines(issueDir.CommentsFile())
	if err != nil {
		t.Fatalf("ReadLines comments.jsonl: %v", err)
	}
	if len(commentLines) != 1 {
		t.Fatalf("comments.jsonl: got %d lines, want 1 (deduped)", len(commentLines))
	}
	if got := commentLines[0].LinearComment.Serialized["body"]; got != "edited" {
		t.Errorf("comments.jsonl: body = %v, want %q (last occurrence kept)", got, "edited")
	}
}

func TestRemoveDriveFile_SluggedDir(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	driveDir := root.AccountFor(account.New("gws", "test")).Drive()

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
	driveDir := root.AccountFor(account.New("gws", "test")).Drive()
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
	driveDir := root.AccountFor(account.New("gws", "test")).Drive()
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
	driveDir := root.AccountFor(account.New("gws", "test")).Drive()
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
	driveDir := root.AccountFor(account.New("gws", "neverbackfilled")).Drive()

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
		if err := s.AppendLine(paths.EmailDateFile(path), gwsEmailLine(id)); err != nil {
			t.Fatalf("AppendLine(%q): %v", id, err)
		}
	}

	lines, err := s.ReadLines(paths.EmailDateFile(path))
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
	if err := s.WriteLines(paths.EmailDateFile(path), initial); err != nil {
		t.Fatalf("WriteLines: %v", err)
	}

	lines, err := s.ReadLines(paths.EmailDateFile(path))
	if err != nil {
		t.Fatalf("ReadLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	// Overwrite with fewer lines — verifies replacement, not append.
	if err := s.WriteLines(paths.EmailDateFile(path), []modelv1.Line{gwsEmailLine("c")}); err != nil {
		t.Fatalf("WriteLines overwrite: %v", err)
	}
	lines, err = s.ReadLines(paths.EmailDateFile(path))
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

	if err := s.WriteLines(paths.EmailDateFile(path), []modelv1.Line{gwsEmailLine("a")}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteLines(paths.EmailDateFile(path), nil); err != nil {
		t.Fatalf("WriteLines empty: %v", err)
	}
	lines, err := s.ReadLines(paths.EmailDateFile(path))
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
	lines, err := s.ReadLines(paths.EmailDateFile(filepath.Join(root.Path(), "nope.jsonl")))
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

	if err := s.AppendLine(paths.EmailDateFile(path), gwsEmailLine("good1")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not valid json\n")
	f.Close()
	if err := s.AppendLine(paths.EmailDateFile(path), gwsEmailLine("good2")); err != nil {
		t.Fatal(err)
	}

	lines, err := s.ReadLines(paths.EmailDateFile(path))
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
		AccountFor(account.New("gws", "test")).
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
	acct := root.AccountFor(account.New("gws", "test"))

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
	acct := root.AccountFor(account.New("gws", "neversynced"))

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
	acct := paths.NewDataRoot(t.TempDir()).AccountFor(account.New("gws", "test"))
	gmailDir := acct.Gmail()

	for _, id := range []string{"msg-1", "msg-2", "msg-3"} {
		if err := s.AppendPendingDelete(gmailDir, id); err != nil {
			t.Fatalf("AppendPendingDelete(%s): %v", id, err)
		}
	}

	data, err := os.ReadFile(gmailDir.PendingDeletesFile().Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "msg-1\nmsg-2\nmsg-3\n"
	if string(data) != want {
		t.Errorf("pending deletes = %q, want %q", data, want)
	}
}

func emailLine(id string, t time.Time, from, subject, text string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID: id, Ts: t, From: from, Subject: subject, Text: text,
		},
	}
}

func TestApplyPendingEmailDeletes(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "test")
	gmailDir := root.AccountFor(acct).Gmail()

	// Write three emails across two date files.
	e1 := emailLine("aaa", ts(2026, 3, 10, 9, 0, 0), "a@x.com", "subj1", "body1")
	e2 := emailLine("bbb", ts(2026, 3, 10, 10, 0, 0), "b@x.com", "subj2", "body2")
	e3 := emailLine("ccc", ts(2026, 3, 11, 8, 0, 0), "c@x.com", "subj3", "body3")

	if err := s.AppendLine(gmailDir.DateFile("2026-03-10"), e1); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(gmailDir.DateFile("2026-03-10"), e2); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLine(gmailDir.DateFile("2026-03-11"), e3); err != nil {
		t.Fatal(err)
	}

	// Mark "aaa" and "ccc" as deleted.
	for _, id := range []string{"aaa", "ccc"} {
		if err := s.AppendPendingDelete(gmailDir, id); err != nil {
			t.Fatal(err)
		}
	}

	// Run maintenance, which should apply the pending deletes.
	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}

	// Pending file should be gone.
	if _, err := os.Stat(gmailDir.PendingDeletesFile().Path()); !os.IsNotExist(err) {
		t.Error("pending deletes file should be removed after maintenance")
	}

	// 2026-03-10 should have only "bbb".
	data, err := os.ReadFile(gmailDir.DateFile("2026-03-10").Path())
	if err != nil {
		t.Fatalf("read date file: %v", err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 {
		t.Fatalf("2026-03-10: got %d lines, want 1", len(lines))
	}
	parsed, err := modelv1.Parse(lines[0])
	if err != nil {
		t.Fatalf("parse remaining line: %v", err)
	}
	if parsed.Email.ID != "bbb" {
		t.Errorf("remaining email ID = %q, want bbb", parsed.Email.ID)
	}

	// 2026-03-11 date file should be removed (all lines deleted).
	if _, err := os.Stat(gmailDir.DateFile("2026-03-11").Path()); !os.IsNotExist(err) {
		t.Error("2026-03-11 file should be removed when all emails deleted")
	}
}

func TestApplyPendingEmailDeletes_NoPendingFile(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "test")
	gmailDir := root.AccountFor(acct).Gmail()

	// Create the gmail dir so Maintain can walk it, but no pending file.
	if err := os.MkdirAll(gmailDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Maintain should succeed even when no pending file exists.
	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestInterleaveThreads_ParentNotInDateFiles verifies that thread replies are
// included in the output even when the parent message's date file is outside
// the selected range (e.g. the parent is from days ago and we're reading with
// --since). This is the fix for the bug where real-time thread replies to old
// messages were silently dropped.
func TestInterleaveThreads_ParentNotInDateFiles(t *testing.T) {
	s, acct := setup(t)

	// Parent message is from March 10 — will be in its own date file.
	parent := msgLine("P1", ts(2026, 3, 10, 9, 0, 0), "Alice", "U1", "old thread start")
	s.Append(acct, "#general", parent)

	// Thread reply is from March 16 — recent.
	reply := modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{
		ID: "R1", Ts: ts(2026, 3, 16, 14, 0, 0), Sender: "Bob", SenderID: "U2", Text: "replying to old thread", Reply: true,
	}}
	s.AppendThread(acct, "#general", "P1", parent)
	s.AppendThread(acct, "#general", "P1", reply)

	// Read only March 16 — the parent's date file (March 10) is NOT selected.
	df, err := s.ReadConversation(acct, "#general", ReadOpts{Date: "2026-03-16"})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// Before the fix this returned 0 messages — the thread reply was silently
	// dropped because the parent wasn't in the selected date files.
	// After the fix: parent + reply from the unmatched thread file are appended.
	if len(df.Messages) < 1 {
		t.Fatalf("messages = %d, want at least 1 (thread reply should not be dropped)", len(df.Messages))
	}

	// Verify the reply is present.
	var foundReply bool
	for _, m := range df.Messages {
		if m.ID == "R1" {
			foundReply = true
			if !m.Reply {
				t.Error("R1 should have Reply=true")
			}
		}
	}
	if !foundReply {
		var ids []string
		for _, m := range df.Messages {
			ids = append(ids, m.ID)
		}
		t.Errorf("thread reply R1 not found in output, got IDs: %v", ids)
	}
}

func TestListGWSServices(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")
	ad := root.AccountFor(acct)

	// Create gmail with 3 date files.
	gmailDir := ad.Gmail().Path()
	if err := os.MkdirAll(gmailDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-04-10.jsonl", "2026-04-11.jsonl", "2026-04-12.jsonl"} {
		if err := os.WriteFile(filepath.Join(gmailDir, d), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create gcalendar with 2 calendar subdirs.
	calDir := filepath.Join(ad.Path(), paths.GcalendarSubdir)
	for _, cal := range []string{"primary", "work"} {
		if err := os.MkdirAll(filepath.Join(calDir, cal), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create gdrive with 1 file subdir.
	driveDir := ad.Drive().Path()
	if err := os.MkdirAll(filepath.Join(driveDir, "my-doc-abc123"), 0o755); err != nil {
		t.Fatal(err)
	}

	infos, err := s.ListGWSServices(acct)
	if err != nil {
		t.Fatalf("ListGWSServices: %v", err)
	}
	if len(infos) != 3 {
		t.Fatalf("got %d services, want 3", len(infos))
	}

	want := map[string]int{
		paths.GmailSubdir:     3,
		paths.GcalendarSubdir: 2,
		paths.GdriveSubdir:    1,
	}
	for _, info := range infos {
		if info.Items != want[info.Service] {
			t.Errorf("%s: got %d items, want %d", info.Service, info.Items, want[info.Service])
		}
	}
}

func TestListGWSServices_Empty(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := NewFSStore(root)
	acct := account.New("gws", "user@example.com")

	infos, err := s.ListGWSServices(acct)
	if err != nil {
		t.Fatalf("ListGWSServices: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("got %d services, want 0", len(infos))
	}
}

// TestReadConversation_OrphanThreadReply_Surfaced verifies that a thread reply
// is still returned even when the thread file never received its parent line
// (e.g. the listener's parent fetch failed). Without the orphan-handling path,
// such replies are silently dropped because Parent.ID is empty.
func TestReadConversation_OrphanThreadReply_Surfaced(t *testing.T) {
	s, acct := setup(t)
	conv := "#general"

	// Old parent on a date file that won't be selected by Since=5m.
	parentTS := "1700000001.000001"
	oldParent := msgLine(parentTS, time.Now().Add(-7*24*time.Hour), "Alice", "U001", "old discussion")
	if err := s.Append(acct, conv, oldParent); err != nil {
		t.Fatalf("Append parent: %v", err)
	}

	// Thread file holds only the reply — parent line was never written.
	recentReply := msgLine("1700000002.000002", time.Now().Add(-10*time.Second), "Bob", "U002", "replying to old thread")
	recentReply.Msg.Reply = true
	if err := s.AppendThread(acct, conv, parentTS, recentReply); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}

	df, err := s.ReadConversation(acct, conv, ReadOpts{Since: 5 * time.Minute})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	var found bool
	for _, m := range df.Messages {
		if m.ID == "1700000002.000002" {
			found = true
			if !m.Reply {
				t.Errorf("orphan reply Reply flag = false, want true")
			}
			if m.Text != "replying to old thread" {
				t.Errorf("Text = %q, want %q", m.Text, "replying to old thread")
			}
		}
	}
	if !found {
		var ids []string
		for _, m := range df.Messages {
			ids = append(ids, m.ID)
		}
		t.Errorf("orphan reply not surfaced; got %d messages: %v", len(df.Messages), ids)
	}
}

// TestReadConversation_ThreadReply_OldParentInDateFile verifies the realistic
// production case: parent is in an old date file outside the Since window,
// the thread file holds parent+reply, and a recent reply must surface alone
// (the old parent is correctly filtered out by the Since cutoff).
func TestReadConversation_ThreadReply_OldParentInDateFile(t *testing.T) {
	s, acct := setup(t)
	conv := "#general"

	parentTS := "1700000001.000001"
	oldParent := msgLine(parentTS, time.Now().Add(-7*24*time.Hour), "Alice", "U001", "old discussion")
	if err := s.Append(acct, conv, oldParent); err != nil {
		t.Fatalf("Append parent: %v", err)
	}
	if err := s.AppendThread(acct, conv, parentTS, oldParent); err != nil {
		t.Fatalf("AppendThread parent: %v", err)
	}
	recentReply := msgLine("1700000002.000002", time.Now().Add(-10*time.Second), "Bob", "U002", "fresh reply")
	recentReply.Msg.Reply = true
	if err := s.AppendThread(acct, conv, parentTS, recentReply); err != nil {
		t.Fatalf("AppendThread reply: %v", err)
	}

	df, err := s.ReadConversation(acct, conv, ReadOpts{Since: 5 * time.Minute})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(df.Messages) != 1 || df.Messages[0].ID != "1700000002.000002" {
		var ids []string
		for _, m := range df.Messages {
			ids = append(ids, m.ID)
		}
		t.Errorf("want only the fresh reply, got %d: %v", len(df.Messages), ids)
	}
}

// TestReadConversation_ThreadReplyToOldParent_DeliveredViaSince verifies that
// a new thread reply to an old parent message is returned by ReadConversation
// with a Since filter. This simulates the hub's drainConversation path where
// lastDelivered is recent but the thread root is days old.
func TestReadConversation_ThreadReplyToOldParent_DeliveredViaSince(t *testing.T) {
	s, acct := setup(t)
	conv := "#general"

	// Parent message written 7 days ago.
	oldParent := msgLine("1700000001.000001", time.Now().Add(-7*24*time.Hour), "Alice", "U001", "old discussion")
	if err := s.Append(acct, conv, oldParent); err != nil {
		t.Fatalf("Append parent: %v", err)
	}

	// New thread reply written just now.
	recentReply := msgLine("1700000002.000002", time.Now().Add(-10*time.Second), "Bob", "U002", "replying to old thread")
	recentReply.Msg.Reply = true
	if err := s.AppendThread(acct, conv, "1700000001.000001", recentReply); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}

	// Read with Since=5 minutes, simulating hub drain window.
	df, err := s.ReadConversation(acct, conv, ReadOpts{Since: 5 * time.Minute})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}

	// The recent thread reply should be present.
	var found bool
	for _, m := range df.Messages {
		if m.ID == "1700000002.000002" {
			found = true
			if m.Text != "replying to old thread" {
				t.Errorf("Text = %q, want %q", m.Text, "replying to old thread")
			}
		}
	}
	if !found {
		t.Errorf("thread reply not found in ReadConversation(Since=5m), got %d messages: %v",
			len(df.Messages), func() []string {
				var ids []string
				for _, m := range df.Messages {
					ids = append(ids, m.ID)
				}
				return ids
			}())
	}
}
