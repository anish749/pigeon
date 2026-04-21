package cli

import (
	"bytes"
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

// writeLines serialises lines through modelv1.Marshal and writes them to
// path. Using the real marshaller keeps these tests honest: if the on-disk
// format ever drifts (field names, timestamp layout), the test files drift
// with it, matching what real pollers produce.
func writeLines(t *testing.T, path string, lines ...modelv1.Line) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, l := range lines {
		data, err := modelv1.Marshal(l)
		if err != nil {
			t.Fatalf("marshal %v: %v", l.Type, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
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
	// Drive grouping is per-doc (by slug), not one collapsed "gdrive" entry.
	if convs[0].Display != "gws/anish/gdrive/notes-ABC" {
		t.Errorf("Display = %q, want gws/anish/gdrive/notes-ABC", convs[0].Display)
	}
	if convs[0].Dir != drive.Path() {
		t.Errorf("Dir = %q, want %s", convs[0].Dir, drive.Path())
	}
	want, _ := time.Parse("2006-01-02", "2026-04-15")
	if !convs[0].LatestTime.Equal(want) {
		t.Errorf("LatestTime = %v, want %v", convs[0].LatestTime, want)
	}
}

// TestExtractConversations_Gmail verifies that gmail date files — which
// contain LineEmail records carrying "ts" — are routed to the emailDateKind
// and contribute the latest email timestamp to LatestTime. Before the
// filekinds refactor, gmail files scanned through modelv1.ParseDateFile
// classified LineEmail as unknown, so LatestTime was always zero (conversation
// showed "active" instead of "last X ago").
func TestExtractConversations_Gmail(t *testing.T) {
	root := t.TempDir()
	acct := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish"))
	now := time.Now()

	path := string(acct.Gmail().DateFile("2026-04-14"))
	writeLines(t, path,
		modelv1.Line{Type: modelv1.LineEmail, Email: &modelv1.EmailLine{
			ID: "e1", ThreadID: "t1", Ts: now.Add(-3 * time.Hour), From: "a@b.c", Subject: "hi",
		}},
		modelv1.Line{Type: modelv1.LineEmail, Email: &modelv1.EmailLine{
			ID: "e2", ThreadID: "t2", Ts: now.Add(-30 * time.Minute), From: "x@y.z", Subject: "yo",
		}},
	)

	convs, err := extractConversations([]string{path}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	// All gmail files for an account collapse into one "gmail" conversation —
	// parts[:3] is platform/account/service.
	if convs[0].Display != "gws/anish/gmail" {
		t.Errorf("Display = %q, want gws/anish/gmail", convs[0].Display)
	}
	age := now.Sub(convs[0].LatestTime)
	if age < 25*time.Minute || age > 35*time.Minute {
		t.Errorf("age = %v, want ~30m (latest email)", age)
	}
}

// TestExtractConversations_Calendar verifies that calendar date files — which
// contain LineEvent records carrying "updated" (not "ts") — are routed to the
// calendarDateKind and contribute the newest "updated" timestamp. Before
// filekinds, calendar events were unclassified by modelv1's DateFile and
// LatestTime was always zero.
func TestExtractConversations_Calendar(t *testing.T) {
	root := t.TempDir()
	cal := paths.NewDataRoot(root).
		AccountFor(account.New("gws", "anish")).
		Calendar("primary")
	now := time.Now()
	olderUpdated := now.Add(-6 * time.Hour).UTC().Format(time.RFC3339)
	newerUpdated := now.Add(-45 * time.Minute).UTC().Format(time.RFC3339)

	path := string(cal.DateFile("2026-04-14"))
	writeLines(t, path,
		modelv1.Line{Type: modelv1.LineEvent, Event: &modelv1.CalendarEvent{
			Serialized: map[string]any{"id": "e1", "updated": olderUpdated, "summary": "standup"},
		}},
		modelv1.Line{Type: modelv1.LineEvent, Event: &modelv1.CalendarEvent{
			Serialized: map[string]any{"id": "e2", "updated": newerUpdated, "summary": "review"},
		}},
	)

	convs, err := extractConversations([]string{path}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	// Calendar grouping is per-calendarID, not one collapsed "gcalendar" entry.
	if convs[0].Display != "gws/anish/gcalendar/primary" {
		t.Errorf("Display = %q, want gws/anish/gcalendar/primary", convs[0].Display)
	}
	if convs[0].Dir != cal.Path() {
		t.Errorf("Dir = %q, want %s", convs[0].Dir, cal.Path())
	}
	age := now.Sub(convs[0].LatestTime)
	if age < 40*time.Minute || age > 50*time.Minute {
		t.Errorf("age = %v, want ~45m (newest updated)", age)
	}
}

// TestExtractConversations_Linear verifies routing + timestamp extraction for
// a Linear issue file, and pins the display behaviour: each Linear issue is
// its own conversation (Dir = the issue file itself). The display drops the
// redundant "issues/" segment so the label reads as platform/workspace/id.
// The test also asserts that the newest timestamp wins even when it comes
// from a comment's "createdAt" rather than the issue's "updatedAt" — Linear
// interleaves both line types in the same file.
func TestExtractConversations_Linear(t *testing.T) {
	root := t.TempDir()
	acct := paths.NewDataRoot(root).AccountFor(account.New("linear-issues", "acme"))
	now := time.Now()
	issueUpdated := now.Add(-5 * time.Hour).UTC().Format(time.RFC3339)
	commentCreated := now.Add(-20 * time.Minute).UTC().Format(time.RFC3339)

	path := string(acct.Linear().IssueFile("PROJ-123"))
	writeLines(t, path,
		modelv1.Line{Type: modelv1.LineLinearIssue, Issue: &modelv1.LinearIssue{
			Serialized: map[string]any{
				"id":         "i1",
				"identifier": "PROJ-123",
				"updatedAt":  issueUpdated,
			},
		}},
		modelv1.Line{Type: modelv1.LineLinearComment, LinearComment: &modelv1.LinearComment{
			Serialized: map[string]any{
				"id":        "c1",
				"createdAt": commentCreated,
			},
		}},
	)

	convs, err := extractConversations([]string{path}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	if convs[0].Display != "linear-issues/acme/PROJ-123" {
		t.Errorf("Display = %q, want linear-issues/acme/PROJ-123", convs[0].Display)
	}
	if convs[0].Dir != path {
		t.Errorf("Dir = %q, want the issue file %s", convs[0].Dir, path)
	}
	age := now.Sub(convs[0].LatestTime)
	if age < 15*time.Minute || age > 25*time.Minute {
		t.Errorf("age = %v, want ~20m (comment createdAt wins over issue updatedAt)", age)
	}
}

// TestExtractConversations_PerIssueLinearGrouping verifies that two Linear
// issue files in the same workspace produce two separate conversation
// entries rather than collapsing into one. This is the per-source grouping
// behaviour: each issue is its own unit of activity.
func TestExtractConversations_PerIssueLinearGrouping(t *testing.T) {
	root := t.TempDir()
	acct := paths.NewDataRoot(root).AccountFor(account.New("linear-issues", "acme"))
	now := time.Now()

	issue1 := string(acct.Linear().IssueFile("PROJ-1"))
	issue2 := string(acct.Linear().IssueFile("PROJ-2"))
	writeLines(t, issue1,
		modelv1.Line{Type: modelv1.LineLinearIssue, Issue: &modelv1.LinearIssue{
			Serialized: map[string]any{"id": "i1", "identifier": "PROJ-1",
				"updatedAt": now.Add(-10 * time.Minute).UTC().Format(time.RFC3339)},
		}},
	)
	writeLines(t, issue2,
		modelv1.Line{Type: modelv1.LineLinearIssue, Issue: &modelv1.LinearIssue{
			Serialized: map[string]any{"id": "i2", "identifier": "PROJ-2",
				"updatedAt": now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)},
		}},
	)

	convs, err := extractConversations([]string{issue1, issue2}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (one per issue)", len(convs))
	}
}

// TestExtractConversations_PerDocDriveGrouping verifies that two Drive docs
// produce two conversation entries rather than collapsing under "gdrive".
func TestExtractConversations_PerDocDriveGrouping(t *testing.T) {
	root := t.TempDir()
	drive := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish")).Drive()

	for _, slug := range []string{"doc-A", "doc-B"} {
		d := drive.File(slug)
		if err := os.MkdirAll(d.Path(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.Path(), "Notes.md"), []byte("# x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d.MetaFile("2026-04-15").Path(), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := []string{
		filepath.Join(drive.File("doc-A").Path(), "Notes.md"),
		filepath.Join(drive.File("doc-B").Path(), "Notes.md"),
	}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (one per doc)", len(convs))
	}
}

// TestExtractConversations_PerCalendarGrouping verifies that two calendars
// under the same gws account produce two conversation entries rather than
// collapsing under "gcalendar".
func TestExtractConversations_PerCalendarGrouping(t *testing.T) {
	root := t.TempDir()
	acct := paths.NewDataRoot(root).AccountFor(account.New("gws", "anish"))
	now := time.Now()

	files := []string{}
	for _, cid := range []string{"primary", "team-cal"} {
		path := string(acct.Calendar(cid).DateFile("2026-04-14"))
		writeLines(t, path,
			modelv1.Line{Type: modelv1.LineEvent, Event: &modelv1.CalendarEvent{
				Serialized: map[string]any{"id": "e1",
					"updated": now.Add(-30 * time.Minute).UTC().Format(time.RFC3339),
					"summary": "meeting"},
			}},
		)
		files = append(files, path)
	}

	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2 (one per calendar)", len(convs))
	}
}

// TestExtractConversations_MixedKinds exercises the full routing matrix in
// one extractConversations call: slack date, slack thread, gmail, calendar,
// drive markdown, and linear issue. Each is expected to resolve to its own
// conversation entry with the right Display and LatestTime.
func TestExtractConversations_MixedKinds(t *testing.T) {
	root := t.TempDir()
	r := paths.NewDataRoot(root)
	now := time.Now()

	// Slack date file.
	slack := r.AccountFor(account.New("slack", "acme")).Conversation("#general")
	slackFile := string(slack.DateFile("2026-04-14"))
	writeDateFile(t, slackFile, []modelv1.MsgLine{msg("m1", now.Add(-10*time.Minute))})

	// Slack thread file under the same conversation.
	slackThread := string(slack.ThreadFile("1742100000"))
	writeThreadFile(t, slackThread,
		msg("p1", now.Add(-3*time.Hour)),
		[]modelv1.MsgLine{msg("r1", now.Add(-5*time.Minute))},
	)

	// Gmail date file.
	gws := r.AccountFor(account.New("gws", "anish"))
	gmailFile := string(gws.Gmail().DateFile("2026-04-14"))
	writeLines(t, gmailFile,
		modelv1.Line{Type: modelv1.LineEmail, Email: &modelv1.EmailLine{
			ID: "e1", ThreadID: "t1", Ts: now.Add(-2 * time.Hour), From: "a@b.c", Subject: "hi",
		}},
	)

	// Calendar date file.
	calFile := string(gws.Calendar("primary").DateFile("2026-04-14"))
	calUpdated := now.Add(-40 * time.Minute).UTC().Format(time.RFC3339)
	writeLines(t, calFile,
		modelv1.Line{Type: modelv1.LineEvent, Event: &modelv1.CalendarEvent{
			Serialized: map[string]any{"id": "e1", "updated": calUpdated, "summary": "standup"},
		}},
	)

	// Drive markdown + sibling meta.
	drive := gws.Drive().File("notes-ABC")
	driveNotes := filepath.Join(drive.Path(), "Notes.md")
	if err := os.MkdirAll(drive.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(driveNotes, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	driveMetaDate := now.AddDate(0, 0, -1).UTC().Format("2006-01-02")
	if err := os.WriteFile(drive.MetaFile(driveMetaDate).Path(), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Linear issue file.
	lin := r.AccountFor(account.New("linear-issues", "acme")).Linear()
	linFile := string(lin.IssueFile("PROJ-7"))
	linUpdated := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	writeLines(t, linFile,
		modelv1.Line{Type: modelv1.LineLinearIssue, Issue: &modelv1.LinearIssue{
			Serialized: map[string]any{"id": "i1", "identifier": "PROJ-7", "updatedAt": linUpdated},
		}},
	)

	files := []string{slackFile, slackThread, gmailFile, calFile, driveNotes, linFile}
	convs, err := extractConversations(files, root)
	if err != nil {
		t.Fatal(err)
	}

	// Build Display → conv lookup for clarity.
	byDisplay := make(map[string]activeConv)
	for _, c := range convs {
		byDisplay[c.Display] = c
	}

	wantDisplays := []string{
		"slack/acme/#general",
		"gws/anish/gmail",
		"gws/anish/gcalendar/primary",
		"gws/anish/gdrive/notes-ABC",
		"linear-issues/acme/PROJ-7",
	}
	for _, d := range wantDisplays {
		if _, ok := byDisplay[d]; !ok {
			t.Errorf("missing conversation %q; got displays: %v", d, keys(byDisplay))
		}
	}

	// Spot-check that each kind's own timestamp flowed through.
	slackAge := now.Sub(byDisplay["slack/acme/#general"].LatestTime)
	if slackAge < 3*time.Minute || slackAge > 7*time.Minute {
		t.Errorf("slack age = %v, want ~5m (newest thread reply)", slackAge)
	}
	gmailAge := now.Sub(byDisplay["gws/anish/gmail"].LatestTime)
	if gmailAge < 110*time.Minute || gmailAge > 130*time.Minute {
		t.Errorf("gmail age = %v, want ~2h", gmailAge)
	}
	calAge := now.Sub(byDisplay["gws/anish/gcalendar/primary"].LatestTime)
	if calAge < 35*time.Minute || calAge > 45*time.Minute {
		t.Errorf("calendar age = %v, want ~40m", calAge)
	}
	linAge := now.Sub(byDisplay["linear-issues/acme/PROJ-7"].LatestTime)
	if linAge < 55*time.Minute || linAge > 65*time.Minute {
		t.Errorf("linear age = %v, want ~1h", linAge)
	}
}

func keys(m map[string]activeConv) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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
