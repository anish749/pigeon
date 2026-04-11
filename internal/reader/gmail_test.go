package reader

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadGmailDedup(t *testing.T) {
	dir := t.TempDir()
	gmailDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Gmail()
	if err := os.MkdirAll(gmailDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	// Write two emails with same ID — second should win.
	writeFile(t, gmailDir.DateFile("2026-04-10").Path(),
		`{"type":"email","id":"abc","threadId":"t1","ts":"2026-04-10T10:00:00Z","from":"a@x.com","fromName":"A","to":["b@x.com"],"subject":"First","labels":["INBOX"],"snippet":"old","text":"old body"}
{"type":"email","id":"abc","threadId":"t1","ts":"2026-04-10T10:00:00Z","from":"a@x.com","fromName":"A","to":["b@x.com"],"subject":"Updated","labels":["INBOX"],"snippet":"new","text":"new body"}
{"type":"email","id":"def","threadId":"t2","ts":"2026-04-10T11:00:00Z","from":"b@x.com","fromName":"B","to":["a@x.com"],"subject":"Other","labels":["INBOX"],"snippet":"other","text":"other body"}
`)

	result, err := ReadGmail(gmailDir, Filters{Date: "2026-04-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Emails) != 2 {
		t.Fatalf("got %d emails, want 2", len(result.Emails))
	}
	if result.Emails[0].Subject != "Updated" {
		t.Errorf("first email subject = %q, want %q", result.Emails[0].Subject, "Updated")
	}
}

func TestReadGmailPendingDeletes(t *testing.T) {
	dir := t.TempDir()
	gmailDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Gmail()
	if err := os.MkdirAll(gmailDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, gmailDir.DateFile("2026-04-10").Path(),
		`{"type":"email","id":"keep","threadId":"t1","ts":"2026-04-10T10:00:00Z","from":"a@x.com","fromName":"A","to":["b@x.com"],"subject":"Keep","labels":["INBOX"],"snippet":"k","text":"k"}
{"type":"email","id":"delete-me","threadId":"t2","ts":"2026-04-10T11:00:00Z","from":"b@x.com","fromName":"B","to":["a@x.com"],"subject":"Delete","labels":["INBOX"],"snippet":"d","text":"d"}
`)

	writeFile(t, gmailDir.PendingDeletesPath(), "delete-me\n")

	result, err := ReadGmail(gmailDir, Filters{Date: "2026-04-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Emails) != 1 {
		t.Fatalf("got %d emails, want 1", len(result.Emails))
	}
	if result.Emails[0].ID != "keep" {
		t.Errorf("remaining email ID = %q, want %q", result.Emails[0].ID, "keep")
	}
}

func TestReadGmailSorted(t *testing.T) {
	dir := t.TempDir()
	gmailDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Gmail()
	if err := os.MkdirAll(gmailDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	// Write out-of-order.
	writeFile(t, gmailDir.DateFile("2026-04-10").Path(),
		`{"type":"email","id":"late","threadId":"t1","ts":"2026-04-10T15:00:00Z","from":"a@x.com","fromName":"A","to":["b@x.com"],"subject":"Late","labels":["INBOX"],"snippet":"","text":""}
{"type":"email","id":"early","threadId":"t2","ts":"2026-04-10T08:00:00Z","from":"b@x.com","fromName":"B","to":["a@x.com"],"subject":"Early","labels":["INBOX"],"snippet":"","text":""}
`)

	result, err := ReadGmail(gmailDir, Filters{Date: "2026-04-10"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Emails) != 2 {
		t.Fatalf("got %d, want 2", len(result.Emails))
	}
	if result.Emails[0].ID != "early" || result.Emails[1].ID != "late" {
		t.Errorf("emails not sorted by time: %v, %v", result.Emails[0].ID, result.Emails[1].ID)
	}
}

func TestReadGmailLastN(t *testing.T) {
	dir := t.TempDir()
	gmailDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Gmail()
	if err := os.MkdirAll(gmailDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	// Write 5 emails.
	var lines string
	for i := 0; i < 5; i++ {
		ts := time.Date(2026, 4, 10, i, 0, 0, 0, time.UTC).Format(time.RFC3339)
		lines += `{"type":"email","id":"` + string(rune('a'+i)) + `","threadId":"t","ts":"` + ts + `","from":"a@x.com","fromName":"A","to":["b@x.com"],"subject":"Msg","labels":["INBOX"],"snippet":"","text":""}` + "\n"
	}
	writeFile(t, gmailDir.DateFile("2026-04-10").Path(), lines)

	result, err := ReadGmail(gmailDir, Filters{Last: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Emails) != 2 {
		t.Fatalf("got %d, want 2", len(result.Emails))
	}
	// Last 2 should be the latest ones.
	if result.Emails[0].ID != "d" || result.Emails[1].ID != "e" {
		t.Errorf("got IDs %q, %q — want d, e", result.Emails[0].ID, result.Emails[1].ID)
	}
}

func TestReadGmailEmptyDir(t *testing.T) {
	dir := t.TempDir()
	gmailDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Gmail()

	result, err := ReadGmail(gmailDir, Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Emails) != 0 {
		t.Fatalf("got %d emails, want 0", len(result.Emails))
	}
}
