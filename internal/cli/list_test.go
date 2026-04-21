package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// writeDateFile creates a date JSONL file with the given messages using the model layer.
func writeDateFile(t *testing.T, path string, msgs []modelv1.MsgLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := modelv1.MarshalDateFile(&modelv1.DateFile{Messages: msgs})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeThreadFile creates a thread JSONL file with a parent and replies using the model layer.
func writeThreadFile(t *testing.T, path string, parent modelv1.MsgLine, replies []modelv1.MsgLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range replies {
		replies[i].Reply = true
	}
	data, err := modelv1.MarshalThreadFile(&modelv1.ThreadFile{Parent: parent, Replies: replies})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func msg(id string, ts time.Time) modelv1.MsgLine {
	return modelv1.MsgLine{ID: id, Ts: ts, Sender: "Test", SenderID: "U123", Text: "hello"}
}

func TestExtractConversations(t *testing.T) {
	sharedRoot := t.TempDir()
	root := paths.NewDataRoot(sharedRoot)
	general := root.AccountFor(account.New("slack", "acme")).Conversation("#general")
	random := root.AccountFor(account.New("slack", "acme")).Conversation("#random")
	alice := root.AccountFor(account.New("whatsapp", "phone")).Conversation("Alice")

	now := time.Now()

	generalDate1 := string(general.DateFile("2026-04-07"))
	generalDate2 := string(general.DateFile("2026-04-06"))
	generalThread := string(general.ThreadFile("1742100000"))
	randomDate := string(random.DateFile("2026-04-07"))
	aliceDate := string(alice.DateFile("2026-04-05"))

	writeDateFile(t, generalDate1, []modelv1.MsgLine{msg("1", now.Add(-1*time.Hour))})
	writeDateFile(t, generalDate2, []modelv1.MsgLine{msg("2", now.Add(-24*time.Hour))})
	writeThreadFile(t, generalThread, msg("3", now.Add(-2*time.Hour)), []modelv1.MsgLine{msg("4", now.Add(-30*time.Minute))})
	writeDateFile(t, randomDate, []modelv1.MsgLine{msg("5", now.Add(-2*time.Hour))})
	writeDateFile(t, aliceDate, []modelv1.MsgLine{msg("6", now.Add(-3*time.Hour))})

	files := []string{generalDate1, generalDate2, generalThread, randomDate, aliceDate}
	convs, err := extractConversations(files, sharedRoot)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 3 {
		t.Fatalf("got %d conversations, want 3", len(convs))
	}

	// #general: thread reply is newest (30m ago), should win over date file (1h ago).
	if convs[0].Display != "slack/acme/#general" {
		t.Errorf("convs[0].Display = %q, want slack/acme/#general", convs[0].Display)
	}
	age := now.Sub(convs[0].LatestTime)
	if age < 25*time.Minute || age > 35*time.Minute {
		t.Errorf("convs[0] age = %v, want ~30m", age)
	}
	if convs[0].Dir != general.Path() {
		t.Errorf("convs[0].Dir = %q, want %s", convs[0].Dir, general.Path())
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
	acct := paths.NewDataRoot(root).AccountFor(account.New("slack", "acme"))
	general := acct.Conversation("#general")
	now := time.Now()

	thread1 := string(general.ThreadFile("1742100000"))
	thread2 := string(general.ThreadFile("1742200000"))

	writeThreadFile(t, thread1, msg("1", now.Add(-3*time.Hour)), []modelv1.MsgLine{msg("2", now.Add(-2*time.Hour))})
	writeThreadFile(t, thread2, msg("3", now.Add(-2*time.Hour)), []modelv1.MsgLine{msg("4", now.Add(-1*time.Hour))})

	files := []string{thread1, thread2}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	// Thread-only conversation should have timestamp from newest reply.
	age := now.Sub(convs[0].LatestTime)
	if age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("thread-only age = %v, want ~1h", age)
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
	acct := paths.NewDataRoot(root).AccountFor(account.New("slack", "acme"))
	threads := acct.Conversation("threads")
	now := time.Now()

	dateFile := string(threads.DateFile("2026-04-07"))
	threadFile := string(threads.ThreadFile("1742100000"))

	writeDateFile(t, dateFile, []modelv1.MsgLine{msg("1", now.Add(-1*time.Hour))})
	writeThreadFile(t, threadFile, msg("2", now.Add(-3*time.Hour)), []modelv1.MsgLine{msg("3", now.Add(-2*time.Hour))})

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
	if convs[0].Dir != threads.Path() {
		t.Errorf("Dir = %q, want %s", convs[0].Dir, threads.Path())
	}
	// Date file message (1h ago) is newer than thread reply (2h ago).
	age := now.Sub(convs[0].LatestTime)
	if age < 55*time.Minute || age > 65*time.Minute {
		t.Errorf("age = %v, want ~1h", age)
	}
}

// TestExtractConversations_DriveContent verifies that a Drive content file
// (e.g. Notes.md) in the file list does NOT cause extractConversations to
// return an error. This is the regression from the issue where list --since
// fed markdown through the JSONL parser.
func TestExtractConversations_DriveContent(t *testing.T) {
	root := t.TempDir()
	drive := paths.NewDataRoot(root).
		AccountFor(account.New("gws", "anish")).
		Drive().File("notes-ABC")

	notesPath := filepath.Join(drive.Path(), "Notes.md")
	if err := os.MkdirAll(drive.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notesPath, []byte("# heading\n- bullet\nplain text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sibling meta gives the drive content its date stamp.
	metaPath := drive.MetaFile("2026-04-15").Path()
	if err := os.WriteFile(metaPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	convs, err := extractConversations([]string{notesPath}, root)
	if err != nil {
		t.Fatalf("drive markdown should not error extractConversations: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	if convs[0].Display != "gws/anish/gdrive" {
		t.Errorf("Display = %q, want gws/anish/gdrive", convs[0].Display)
	}
	want, _ := time.Parse("2006-01-02", "2026-04-15")
	if !convs[0].LatestTime.Equal(want) {
		t.Errorf("LatestTime = %v, want %v", convs[0].LatestTime, want)
	}
}

func TestExtractConversations_PreservesOrder(t *testing.T) {
	root := t.TempDir()
	acct := paths.NewDataRoot(root).AccountFor(account.New("slack", "acme"))
	now := time.Now()

	beta := string(acct.Conversation("#beta").DateFile("2026-04-07"))
	alpha := string(acct.Conversation("#alpha").DateFile("2026-04-07"))

	writeDateFile(t, beta, []modelv1.MsgLine{msg("1", now.Add(-1*time.Hour))})
	writeDateFile(t, alpha, []modelv1.MsgLine{msg("2", now.Add(-2*time.Hour))})

	files := []string{beta, alpha}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convs))
	}
	if convs[0].Display != "slack/acme/#beta" {
		t.Errorf("first conversation = %q, want slack/acme/#beta", convs[0].Display)
	}
}
