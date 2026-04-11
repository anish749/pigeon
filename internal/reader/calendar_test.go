package reader

import (
	"os"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

func TestReadCalendarDedup(t *testing.T) {
	dir := t.TempDir()
	calDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Calendar("primary")
	if err := os.MkdirAll(calDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	today := time.Now().Format("2006-01-02")
	writeFile(t, calDir.DateFile(today).Path(),
		`{"type":"event","id":"evt1","status":"confirmed","summary":"Old title","start":{"dateTime":"`+today+`T10:00:00Z"},"end":{"dateTime":"`+today+`T11:00:00Z"},"updated":"`+today+`T08:00:00Z"}
{"type":"event","id":"evt1","status":"confirmed","summary":"Updated title","start":{"dateTime":"`+today+`T10:00:00Z"},"end":{"dateTime":"`+today+`T11:00:00Z"},"updated":"`+today+`T09:00:00Z"}
{"type":"event","id":"evt2","status":"confirmed","summary":"Second event","start":{"dateTime":"`+today+`T14:00:00Z"},"end":{"dateTime":"`+today+`T15:00:00Z"},"updated":"`+today+`T08:00:00Z"}
`)

	result, err := ReadCalendar(calDir, Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(result.Events))
	}
	if result.Events[0].Runtime.Summary != "Updated title" {
		t.Errorf("first event summary = %q, want %q", result.Events[0].Runtime.Summary, "Updated title")
	}
}

func TestReadCalendarExcludeCancelled(t *testing.T) {
	dir := t.TempDir()
	calDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Calendar("primary")
	if err := os.MkdirAll(calDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	today := time.Now().Format("2006-01-02")
	writeFile(t, calDir.DateFile(today).Path(),
		`{"type":"event","id":"live","status":"confirmed","summary":"Live event","start":{"dateTime":"`+today+`T10:00:00Z"},"end":{"dateTime":"`+today+`T11:00:00Z"}}
{"type":"event","id":"dead","status":"cancelled","summary":"Cancelled event","start":{"dateTime":"`+today+`T14:00:00Z"},"end":{"dateTime":"`+today+`T15:00:00Z"}}
`)

	result, err := ReadCalendar(calDir, Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(result.Events))
	}
	if result.Events[0].Runtime.Id != "live" {
		t.Errorf("event ID = %q, want %q", result.Events[0].Runtime.Id, "live")
	}
}

func TestReadCalendarSortedByStart(t *testing.T) {
	dir := t.TempDir()
	calDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Calendar("primary")
	if err := os.MkdirAll(calDir.Path(), 0755); err != nil {
		t.Fatal(err)
	}

	today := time.Now().Format("2006-01-02")
	writeFile(t, calDir.DateFile(today).Path(),
		`{"type":"event","id":"afternoon","status":"confirmed","summary":"Afternoon","start":{"dateTime":"`+today+`T14:00:00Z"},"end":{"dateTime":"`+today+`T15:00:00Z"}}
{"type":"event","id":"morning","status":"confirmed","summary":"Morning","start":{"dateTime":"`+today+`T09:00:00Z"},"end":{"dateTime":"`+today+`T10:00:00Z"}}
`)

	result, err := ReadCalendar(calDir, Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("got %d, want 2", len(result.Events))
	}
	if result.Events[0].Runtime.Id != "morning" {
		t.Errorf("first event = %q, want morning", result.Events[0].Runtime.Id)
	}
}

func TestReadCalendarEmptyDir(t *testing.T) {
	dir := t.TempDir()
	calDir := paths.NewDataRoot(dir).Platform("gws").AccountFromSlug("test").Calendar("primary")

	result, err := ReadCalendar(calDir, Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("got %d events, want 0", len(result.Events))
	}
}
