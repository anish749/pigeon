package read

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

// writeKindTestFile is a small helper for tests in this file.
func writeKindTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKindMatch_Routing(t *testing.T) {
	root := t.TempDir()
	r := paths.NewDataRoot(root)

	slackConv := r.AccountFor(account.New("slack", "acme")).Conversation("#general")
	gmailAcct := r.AccountFor(account.New("gws", "anish"))

	cases := []struct {
		name string
		path string
		want string // expected Kind.Name()
	}{
		{
			name: "slack date file",
			path: string(slackConv.DateFile("2026-04-14")),
			want: "messaging-date",
		},
		{
			name: "slack thread file",
			path: string(slackConv.ThreadFile("1742100000")),
			want: "thread",
		},
		{
			name: "gmail date file",
			path: string(gmailAcct.Gmail().DateFile("2026-04-14")),
			want: "email-date",
		},
		{
			name: "calendar date file",
			path: string(gmailAcct.Calendar("primary").DateFile("2026-04-14")),
			want: "calendar-date",
		},
		{
			name: "drive markdown",
			path: filepath.Join(gmailAcct.Drive().File("notes-ABC").Path(), "Notes.md"),
			want: "drive-content",
		},
		{
			name: "drive csv",
			path: filepath.Join(gmailAcct.Drive().File("sheet-ABC").Path(), "Sheet1.csv"),
			want: "drive-content",
		},
		{
			name: "drive comments jsonl",
			path: filepath.Join(gmailAcct.Drive().File("doc-ABC").Path(), "comments.jsonl"),
			want: "drive-content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var matched Kind
			for _, k := range kinds {
				if k.Match(tc.path) {
					matched = k
					break
				}
			}
			if matched == nil {
				t.Fatalf("no kind matched %s", tc.path)
			}
			if matched.Name() != tc.want {
				t.Errorf("matched %s, want %s", matched.Name(), tc.want)
			}
		})
	}
}

func TestKindMatch_UnknownFile(t *testing.T) {
	root := t.TempDir()
	// Random file not under any recognised directory layout.
	path := filepath.Join(root, "random.txt")
	for _, k := range kinds {
		if k.Match(path) {
			t.Errorf("%s unexpectedly matched random.txt", k.Name())
		}
	}
}

func TestLatestTs_MessagingDateFile(t *testing.T) {
	dir := t.TempDir()
	conv := paths.NewDataRoot(dir).AccountFor(account.New("slack", "acme")).Conversation("#general")
	path := string(conv.DateFile("2026-04-14"))

	// Two messages and a reaction. Latest ts wins.
	writeKindTestFile(t, path, `{"type":"msg","id":"1","ts":"2026-04-14T08:00:00Z","sender":"a","from":"U1","text":"hi"}
{"type":"msg","id":"2","ts":"2026-04-14T10:00:00Z","sender":"b","from":"U2","text":"yo"}
{"type":"react","ts":"2026-04-14T11:30:00Z","msg":"2","sender":"c","from":"U3","emoji":"+1"}
`)

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-14T11:30:00Z")
	if !ts.Equal(want) {
		t.Errorf("got %v, want %v", ts, want)
	}
}

func TestLatestTs_ThreadFile(t *testing.T) {
	dir := t.TempDir()
	conv := paths.NewDataRoot(dir).AccountFor(account.New("slack", "acme")).Conversation("#general")
	path := string(conv.ThreadFile("1742100000"))

	writeKindTestFile(t, path, `{"type":"msg","id":"p","ts":"2026-04-14T08:00:00Z","sender":"a","from":"U1","text":"parent"}
{"type":"msg","id":"r1","ts":"2026-04-14T09:00:00Z","sender":"b","from":"U2","text":"reply","reply":true}
{"type":"msg","id":"r2","ts":"2026-04-14T09:30:00Z","sender":"c","from":"U3","text":"reply2","reply":true}
`)

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-14T09:30:00Z")
	if !ts.Equal(want) {
		t.Errorf("got %v, want %v", ts, want)
	}
}

func TestLatestTs_EmailDateFile(t *testing.T) {
	dir := t.TempDir()
	acct := paths.NewDataRoot(dir).AccountFor(account.New("gws", "anish"))
	path := string(acct.Gmail().DateFile("2026-04-14"))

	writeKindTestFile(t, path, `{"type":"email","id":"e1","threadId":"t1","ts":"2026-04-14T07:00:00Z","from":"a@b.c","subject":"hi"}
{"type":"email","id":"e2","threadId":"t2","ts":"2026-04-14T12:00:00Z","from":"x@y.z","subject":"yo"}
`)

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-14T12:00:00Z")
	if !ts.Equal(want) {
		t.Errorf("got %v, want %v", ts, want)
	}
}

func TestLatestTs_DriveMarkdownUsesSiblingMeta(t *testing.T) {
	dir := t.TempDir()
	fileDir := paths.NewDataRoot(dir).
		AccountFor(account.New("gws", "anish")).
		Drive().File("notes-ABC")

	notesPath := filepath.Join(fileDir.Path(), "Notes.md")
	writeKindTestFile(t, notesPath, "# this is markdown\n- not jsonl\n")

	// Sibling meta file — driveContentKind reads the date from its filename.
	metaPath := string(fileDir.MetaFile("2026-04-15").Path())
	writeKindTestFile(t, metaPath, `{}`)

	ts, err := LatestTs(notesPath)
	if err != nil {
		t.Fatalf("markdown file should not error: %v", err)
	}
	want, _ := time.Parse("2006-01-02", "2026-04-15")
	if !ts.Equal(want) {
		t.Errorf("got %v, want %v", ts, want)
	}
}

func TestLatestTs_DriveMissingMetaReturnsZero(t *testing.T) {
	dir := t.TempDir()
	fileDir := paths.NewDataRoot(dir).
		AccountFor(account.New("gws", "anish")).
		Drive().File("notes-ABC")

	notesPath := filepath.Join(fileDir.Path(), "Notes.md")
	writeKindTestFile(t, notesPath, "# markdown with no sibling meta")

	ts, err := LatestTs(notesPath)
	if err != nil {
		t.Fatalf("missing meta should not error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("got %v, want zero time", ts)
	}
}

func TestLatestTs_CalendarReturnsZero(t *testing.T) {
	dir := t.TempDir()
	cal := paths.NewDataRoot(dir).
		AccountFor(account.New("gws", "anish")).
		Calendar("primary")
	path := string(cal.DateFile("2026-04-14"))

	// Calendar events lack a "ts" field; the kind is registered but
	// deliberately returns zero.
	writeKindTestFile(t, path, `{"type":"event","updated":"2026-04-14T12:00:00Z","summary":"meeting"}
`)

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.IsZero() {
		t.Errorf("got %v, want zero time", ts)
	}
}

func TestLatestTs_UnknownFileReturnsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "random.txt")
	writeKindTestFile(t, path, "anything")

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ts.IsZero() {
		t.Errorf("got %v, want zero", ts)
	}
}

func TestLatestTs_JSONLWithMalformedLines(t *testing.T) {
	dir := t.TempDir()
	conv := paths.NewDataRoot(dir).AccountFor(account.New("slack", "acme")).Conversation("#general")
	path := string(conv.DateFile("2026-04-14"))

	writeKindTestFile(t, path, `{"type":"msg","id":"1","ts":"2026-04-14T08:00:00Z","sender":"a","from":"U1","text":"hi"}
not json at all
{"incomplete":
{"type":"msg","id":"2","ts":"2026-04-14T09:00:00Z","sender":"b","from":"U2","text":"ok"}
`)

	ts, err := LatestTs(path)
	if err != nil {
		t.Fatalf("malformed lines should be skipped silently: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-14T09:00:00Z")
	if !ts.Equal(want) {
		t.Errorf("got %v, want %v", ts, want)
	}
}
