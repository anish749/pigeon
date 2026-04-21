package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	calendar "google.golang.org/api/calendar/v3"
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

func writeLines(t *testing.T, path string, lines []modelv1.Line) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for _, line := range lines {
		raw, err := modelv1.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, raw...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRawFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
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

func TestExtractConversations_GWSSourcesAndLinear(t *testing.T) {
	rootDir := t.TempDir()
	root := paths.NewDataRoot(rootDir)

	gwsAcct := root.AccountFor(account.New("gws", "user-at-example-com"))
	linearAcct := root.AccountFor(account.New("linear-issues", "team"))

	gmailTS := time.Date(2026, 4, 7, 9, 15, 0, 0, time.UTC)
	calendarTS := time.Date(2026, 4, 7, 13, 30, 0, 0, time.UTC)
	driveTS := time.Date(2026, 4, 7, 16, 45, 0, 0, time.UTC)
	issueTS := time.Date(2026, 4, 7, 18, 5, 0, 0, time.UTC)

	gmailFile := gwsAcct.Gmail().DateFile("2026-04-07").Path()
	calendarFile := gwsAcct.Calendar("primary").DateFile("2026-04-07").Path()
	driveFile := gwsAcct.Drive().File("recent-doc-FILEID1")
	notesFile := driveFile.TabFile("Notes").Path()
	commentsFile := driveFile.CommentsFile().Path()
	issueFile := linearAcct.Linear().IssueFile("ENG-101").Path()

	writeLines(t, gmailFile, []modelv1.Line{
		{
			Type: modelv1.LineEmail,
			Email: &modelv1.EmailLine{
				ID:       "email-1",
				ThreadID: "thread-1",
				Ts:       gmailTS,
				From:     "alice@example.com",
				Subject:  "Recent email",
			},
		},
	})

	writeLines(t, calendarFile, []modelv1.Line{
		{
			Type: modelv1.LineEvent,
			Event: &modelv1.CalendarEvent{
				Runtime: calendar.Event{
					Id:      "event-1",
					Updated: calendarTS.Format(time.RFC3339),
					Start:   &calendar.EventDateTime{DateTime: calendarTS.Format(time.RFC3339)},
				},
				Serialized: map[string]any{
					"id":      "event-1",
					"updated": calendarTS.Format(time.RFC3339),
					"start": map[string]any{
						"dateTime": calendarTS.Format(time.RFC3339),
					},
				},
			},
		},
	})

	writeRawFile(t, driveFile.MetaFile("2026-04-07").Path(), `{"fileId":"FILEID1","title":"Recent Doc","modifiedTime":"`+driveTS.Format(time.RFC3339)+`"}`+"\n")
	writeRawFile(t, notesFile, "## Notes\n\nMarkdown content that should not be parsed as JSON.\n")
	writeRawFile(t, commentsFile, "")

	writeLines(t, issueFile, []modelv1.Line{
		{
			Type: modelv1.LineLinearIssue,
			Issue: &modelv1.LinearIssue{
				Runtime: modelv1.LinearIssueRuntime{
					ID:         "issue-1",
					Identifier: "ENG-101",
					UpdatedAt:  issueTS.Format(time.RFC3339),
				},
				Serialized: map[string]any{
					"id":         "issue-1",
					"identifier": "ENG-101",
					"updatedAt":  issueTS.Format(time.RFC3339),
				},
			},
		},
	})

	files := []string{gmailFile, calendarFile, notesFile, commentsFile, issueFile}
	convs, err := extractConversations(files, rootDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(convs) != 4 {
		t.Fatalf("got %d sources, want 4: %+v", len(convs), convs)
	}

	if convs[0].Display != "gws/user-at-example-com/gmail" {
		t.Errorf("convs[0].Display = %q, want gws/user-at-example-com/gmail", convs[0].Display)
	}
	if convs[0].Dir != gwsAcct.Gmail().Path() {
		t.Errorf("convs[0].Dir = %q, want %q", convs[0].Dir, gwsAcct.Gmail().Path())
	}
	if !convs[0].LatestTime.Equal(gmailTS) {
		t.Errorf("convs[0].LatestTime = %v, want %v", convs[0].LatestTime, gmailTS)
	}

	if convs[1].Display != "gws/user-at-example-com/gcalendar/primary" {
		t.Errorf("convs[1].Display = %q, want gws/user-at-example-com/gcalendar/primary", convs[1].Display)
	}
	if convs[1].Dir != gwsAcct.Calendar("primary").Path() {
		t.Errorf("convs[1].Dir = %q, want %q", convs[1].Dir, gwsAcct.Calendar("primary").Path())
	}
	if !convs[1].LatestTime.Equal(calendarTS) {
		t.Errorf("convs[1].LatestTime = %v, want %v", convs[1].LatestTime, calendarTS)
	}

	if convs[2].Display != "gws/user-at-example-com/gdrive/recent-doc-FILEID1" {
		t.Errorf("convs[2].Display = %q, want gws/user-at-example-com/gdrive/recent-doc-FILEID1", convs[2].Display)
	}
	if convs[2].Dir != driveFile.Path() {
		t.Errorf("convs[2].Dir = %q, want %q", convs[2].Dir, driveFile.Path())
	}
	if !convs[2].LatestTime.Equal(driveTS) {
		t.Errorf("convs[2].LatestTime = %v, want %v", convs[2].LatestTime, driveTS)
	}

	if convs[3].Display != "linear-issues/team/ENG-101" {
		t.Errorf("convs[3].Display = %q, want linear-issues/team/ENG-101", convs[3].Display)
	}
	if convs[3].Dir != issueFile {
		t.Errorf("convs[3].Dir = %q, want %q", convs[3].Dir, issueFile)
	}
	if !convs[3].LatestTime.Equal(issueTS) {
		t.Errorf("convs[3].LatestTime = %v, want %v", convs[3].LatestTime, issueTS)
	}
}
