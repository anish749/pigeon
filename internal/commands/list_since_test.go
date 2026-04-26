package commands

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

func msgLine(id string, ts time.Time) modelv1.MsgLine {
	return modelv1.MsgLine{ID: id, Ts: ts, Sender: "Test", SenderID: "U123", Text: "hello"}
}

func TestExtractConversations(t *testing.T) {
	sharedRoot := t.TempDir()
	root := paths.NewDataRoot(sharedRoot)
	general := root.AccountFor(account.New("slack", "acme")).Conversation("#general")
	random := root.AccountFor(account.New("slack", "acme")).Conversation("#random")
	alice := root.AccountFor(account.New("whatsapp", "phone")).Conversation("Alice")

	now := time.Now()

	generalDate1 := general.DateFile("2026-04-07")
	generalDate2 := general.DateFile("2026-04-06")
	generalThread := general.ThreadFile("1742100000")
	randomDate := random.DateFile("2026-04-07")
	aliceDate := alice.DateFile("2026-04-05")

	writeDateFile(t, generalDate1.Path(), []modelv1.MsgLine{msgLine("1", now.Add(-1*time.Hour))})
	writeDateFile(t, generalDate2.Path(), []modelv1.MsgLine{msgLine("2", now.Add(-24*time.Hour))})
	writeThreadFile(t, generalThread.Path(), msgLine("3", now.Add(-2*time.Hour)), []modelv1.MsgLine{msgLine("4", now.Add(-30*time.Minute))})
	writeDateFile(t, randomDate.Path(), []modelv1.MsgLine{msgLine("5", now.Add(-2*time.Hour))})
	writeDateFile(t, aliceDate.Path(), []modelv1.MsgLine{msgLine("6", now.Add(-3*time.Hour))})

	files := []paths.DataFile{generalDate1, generalDate2, generalThread, randomDate, aliceDate}
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

	thread1 := general.ThreadFile("1742100000")
	thread2 := general.ThreadFile("1742200000")

	writeThreadFile(t, thread1.Path(), msgLine("1", now.Add(-3*time.Hour)), []modelv1.MsgLine{msgLine("2", now.Add(-2*time.Hour))})
	writeThreadFile(t, thread2.Path(), msgLine("3", now.Add(-2*time.Hour)), []modelv1.MsgLine{msgLine("4", now.Add(-1*time.Hour))})

	files := []paths.DataFile{thread1, thread2}
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

	dateFile := threads.DateFile("2026-04-07")
	threadFile := threads.ThreadFile("1742100000")

	writeDateFile(t, dateFile.Path(), []modelv1.MsgLine{msgLine("1", now.Add(-1*time.Hour))})
	writeThreadFile(t, threadFile.Path(), msgLine("2", now.Add(-3*time.Hour)), []modelv1.MsgLine{msgLine("3", now.Add(-2*time.Hour))})

	files := []paths.DataFile{dateFile, threadFile}
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

	beta := acct.Conversation("#beta").DateFile("2026-04-07")
	alpha := acct.Conversation("#alpha").DateFile("2026-04-07")

	writeDateFile(t, beta.Path(), []modelv1.MsgLine{msgLine("1", now.Add(-1*time.Hour))})
	writeDateFile(t, alpha.Path(), []modelv1.MsgLine{msgLine("2", now.Add(-2*time.Hour))})

	files := []paths.DataFile{beta, alpha}
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

// --- LatestTs dispatch ---

func TestLatestTs_MessagingDateFile_ScansTs(t *testing.T) {
	root := t.TempDir()
	conv := paths.NewDataRoot(root).AccountFor(account.New("slack", "acme")).Conversation("#general")
	df := conv.DateFile("2026-04-07")

	now := time.Now().UTC().Truncate(time.Second)
	writeDateFile(t, df.Path(), []modelv1.MsgLine{
		msgLine("1", now.Add(-2*time.Hour)),
		msgLine("2", now.Add(-30*time.Minute)),
	})

	got, err := LatestTs(df)
	if err != nil {
		t.Fatalf("LatestTs: %v", err)
	}
	want := now.Add(-30 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLatestTs_CalendarDateFile_ScansUpdated(t *testing.T) {
	root := t.TempDir()
	cal := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish")).Calendar("primary").DateFile("2026-04-07")

	if err := os.MkdirAll(filepath.Dir(cal.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"id":"e1","updated":"2026-04-07T10:00:00Z","created":"2026-04-01T08:00:00Z"}` + "\n" +
		`{"id":"e2","updated":"2026-04-07T15:30:00Z","created":"2026-04-05T08:00:00Z"}` + "\n"
	if err := os.WriteFile(cal.Path(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LatestTs(cal)
	if err != nil {
		t.Fatalf("LatestTs: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-07T15:30:00Z")
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLatestTs_DriveContent_UsesSiblingMetaDate(t *testing.T) {
	root := t.TempDir()
	driveFile := paths.NewDataRoot(root).
		AccountFor(account.New("gws", "anish")).
		Drive().File("doc-abc")

	// Two meta files — newest date should win.
	if err := os.MkdirAll(driveFile.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driveFile.MetaFile("2026-04-01").Path(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driveFile.MetaFile("2026-04-07").Path(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := driveFile.TabFile("Notes")
	if err := os.WriteFile(tab.Path(), []byte("# heading\n\nstuff"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LatestTs(tab)
	if err != nil {
		t.Fatalf("LatestTs: %v", err)
	}
	want, _ := time.Parse("2006-01-02", "2026-04-07")
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestLatestTs_CommentsFile_UsesSiblingMetaDate is a regression for a bug
// where CommentsFile was bucketed with messaging/email/thread in LatestTs
// and scanned for "ts" — but Drive comments JSONL lines carry createdTime
// and modifiedTime, never "ts". Effect: any Drive doc whose only recent
// activity was a comment thread would show a zero LatestTs and a stale
// age in pigeon list --since. Fix: treat CommentsFile like other Drive
// content (TabFile, SheetFile, FormulaFile) — the per-doc drive-meta
// sidecar is the canonical "when did this doc change" anchor and is
// rewritten in the same handler that rewrites comments.jsonl.
func TestLatestTs_CommentsFile_UsesSiblingMetaDate(t *testing.T) {
	root := t.TempDir()
	driveFile := paths.NewDataRoot(root).
		AccountFor(account.New("gws", "anish")).
		Drive().File("doc-abc")
	if err := os.MkdirAll(driveFile.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driveFile.MetaFile("2026-04-07").Path(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real-shaped Drive comment line — carries createdTime / modifiedTime,
	// no "ts" field. The previous dispatch would scan for "ts" and find
	// nothing, returning zero.
	comments := driveFile.CommentsFile()
	body := `{"id":"c1","author":{"displayName":"x"},"content":"hi","createdTime":"2026-04-07T10:00:00Z","modifiedTime":"2026-04-07T10:00:00Z"}` + "\n"
	if err := os.WriteFile(comments.Path(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LatestTs(comments)
	if err != nil {
		t.Fatalf("LatestTs: %v", err)
	}
	want, _ := time.Parse("2006-01-02", "2026-04-07")
	if !got.Equal(want) {
		t.Errorf("got %v, want %v (sibling drive-meta date)", got, want)
	}
}

func TestLatestTs_DriveMarkdown_DoesNotParseAsJSONL(t *testing.T) {
	// Regression: the original list --since crash. Drive markdown was being
	// fed through ParseDateFile and failing on '#' chars. With the typed
	// dispatch, TabFile uses the sibling drive-meta date and never opens
	// the .md.
	root := t.TempDir()
	driveFile := paths.NewDataRoot(root).
		AccountFor(account.New("gws", "anish")).
		Drive().File("doc-abc")
	if err := os.MkdirAll(driveFile.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driveFile.MetaFile("2026-04-07").Path(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := driveFile.TabFile("Notes")
	if err := os.WriteFile(tab.Path(), []byte("# Heading\n\nMarkdown body that is *not* JSON.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LatestTs(tab); err != nil {
		t.Errorf("LatestTs on Drive markdown should not error: %v", err)
	}
}

func TestLatestTs_StandaloneKindsReturnZero(t *testing.T) {
	// Sidecars and bookkeeping files don't carry a "latest activity"
	// timestamp for the list --since use case — they should return zero
	// without error. Mirror the explicit-skip case in LatestTs's switch.
	cases := []struct {
		name string
		file paths.DataFile
	}{
		{"MaintenanceFile", paths.MaintenanceFile("/dev/null/.maintenance.json")},
		{"SyncCursorsFile", paths.SyncCursorsFile("/dev/null/.sync-cursors.yaml")},
		{"PollMetricsFile", paths.PollMetricsFile("/dev/null/.poll-metrics.jsonl")},
		{"PendingDeletesFile", paths.PendingDeletesFile("/dev/null/.pending-email-deletes")},
		{"PeopleFile", paths.PeopleFile("/dev/null/identity/people.jsonl")},
		{"AttachmentFile", paths.AttachmentFile("/dev/null/attachments/img.png")},
		{"ConvMetaFile", paths.ConvMetaFile("/dev/null/.meta.json")},
		{"WorkstreamsFile", paths.WorkstreamsFile("/dev/null/.workspaces/x/workstream/workstreams.json")},
		{"WorkstreamProposalsFile", paths.WorkstreamProposalsFile("/dev/null/.workspaces/x/workstream/proposals.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LatestTs(tc.file)
			if err != nil {
				t.Errorf("LatestTs errored: %v", err)
			}
			if !got.IsZero() {
				t.Errorf("LatestTs = %v, want zero", got)
			}
		})
	}
}

// --- per-source conversation grouping ---

func TestExtractConversations_PerDocDriveGrouping(t *testing.T) {
	// Two Drive docs under the same account should produce two
	// conversations, not collapse to one.
	root := t.TempDir()
	drive := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish")).Drive()
	docA := drive.File("doc-A")
	docB := drive.File("doc-B")
	for _, p := range []string{docA.Path(), docB.Path()} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(docA.MetaFile("2026-04-07").Path(), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docB.MetaFile("2026-04-06").Path(), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	tabA := docA.TabFile("Notes")
	tabB := docB.TabFile("Notes")
	for _, p := range []string{tabA.Path(), tabB.Path()} {
		if err := os.WriteFile(p, []byte("# heading"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	convs, err := extractConversations([]paths.DataFile{tabA, tabB}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (per-doc): %+v", len(convs), convs)
	}
}

func TestExtractConversations_PerCalendarGrouping(t *testing.T) {
	// Two calendars under the same account should produce two conversations.
	root := t.TempDir()
	gws := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish"))
	primary := gws.Calendar("primary").DateFile("2026-04-07")
	team := gws.Calendar("team@example.com").DateFile("2026-04-07")

	body := `{"id":"e1","updated":"2026-04-07T10:00:00Z"}` + "\n"
	for _, df := range []paths.CalendarDateFile{primary, team} {
		if err := os.MkdirAll(filepath.Dir(df.Path()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(df.Path(), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	convs, err := extractConversations([]paths.DataFile{primary, team}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (per-calendar): %+v", len(convs), convs)
	}
}

func TestExtractConversations_PerIssueLinearGrouping(t *testing.T) {
	root := t.TempDir()
	linear := paths.NewDataRoot(root).AccountFor(account.New("linear-issues", "acme")).Linear()
	issue1 := linear.Issue("PROJ-123").DateFile("2026-04-07")
	issue2 := linear.Issue("PROJ-124").DateFile("2026-04-07")

	body := `{"updatedAt":"2026-04-07T10:00:00Z"}` + "\n"
	for _, f := range []paths.LinearDateFile{issue1, issue2} {
		if err := os.MkdirAll(filepath.Dir(f.Path()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.Path(), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	convs, err := extractConversations([]paths.DataFile{issue1, issue2}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (per-issue): %+v", len(convs), convs)
	}
	// Display should drop the redundant /issues/ segment and group at the
	// per-issue directory.
	for _, c := range convs {
		if !filepath.IsAbs(c.Dir) {
			t.Errorf("Dir should be absolute, got %q", c.Dir)
		}
		if !(c.Display == "linear-issues/acme/PROJ-123" || c.Display == "linear-issues/acme/PROJ-124") {
			t.Errorf("unexpected display: %q", c.Display)
		}
	}
}

// TestExtractConversations_PerIssueLinearGrouping_MultiDay verifies that
// multiple date files for the same issue collapse to one conversation
// keyed by the per-issue dir, with LatestTime taken from the newest file.
func TestExtractConversations_PerIssueLinearGrouping_MultiDay(t *testing.T) {
	root := t.TempDir()
	linear := paths.NewDataRoot(root).AccountFor(account.New("linear-issues", "acme")).Linear()
	day1 := linear.Issue("PROJ-123").DateFile("2026-04-06")
	day2 := linear.Issue("PROJ-123").DateFile("2026-04-07")

	bodies := map[paths.LinearDateFile]string{
		day1: `{"updatedAt":"2026-04-06T10:00:00Z"}` + "\n",
		day2: `{"updatedAt":"2026-04-07T15:30:00Z"}` + "\n",
	}
	for f, body := range bodies {
		if err := os.MkdirAll(filepath.Dir(f.Path()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.Path(), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	convs, err := extractConversations([]paths.DataFile{day1, day2}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1 (same issue, two dates): %+v", len(convs), convs)
	}
	if convs[0].Display != "linear-issues/acme/PROJ-123" {
		t.Errorf("Display = %q, want linear-issues/acme/PROJ-123", convs[0].Display)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-07T15:30:00Z")
	if !convs[0].LatestTime.Equal(want) {
		t.Errorf("LatestTime = %v, want %v", convs[0].LatestTime, want)
	}
}

// TestDispatch_UnknownKindErrors verifies that both type-switch dispatchers
// fail loud when handed a DataFile they do not enumerate. nil is the only
// value that satisfies paths.DataFile but matches no concrete case (every
// real implementor is enumerated in the switch), so it exercises the
// default branch — the regression guard against silently dropping a future
// typed kind that someone forgets to wire into the dispatch.
func TestDispatch_UnknownKindErrors(t *testing.T) {
	var nilFile paths.DataFile
	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "LatestTs",
			call: func() error { _, err := LatestTs(nilFile); return err },
		},
		{
			name: "listConvFor",
			call: func() error { _, _, err := listConvFor(nilFile, "/data"); return err },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Error("expected error for unhandled DataFile kind")
			}
		})
	}
}

func TestLatestTs_MalformedJSONLFailsLoud(t *testing.T) {
	root := t.TempDir()
	conv := paths.NewDataRoot(root).AccountFor(account.New("slack", "acme")).Conversation("#general")
	df := conv.DateFile("2026-04-07")
	if err := os.MkdirAll(filepath.Dir(df.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(df.Path(), []byte("not valid json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestTs(df); err == nil {
		t.Error("LatestTs should error on malformed JSONL")
	}
}
