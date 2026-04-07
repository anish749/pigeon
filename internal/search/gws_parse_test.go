package search

import (
	"testing"
)

func TestParseFilePath_GWSDoc(t *testing.T) {
	// 5 segments: gws/account/gdrive/doc-slug/Tab.md
	platform, account, conversation, date, thread, err := ParseFilePath(
		"/data/gws/user/gdrive/my-doc-1abc/Tab 1.md",
		"/data",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if platform != "gws" {
		t.Errorf("platform = %q, want %q", platform, "gws")
	}
	if account != "user" {
		t.Errorf("account = %q, want %q", account, "user")
	}
	if conversation != "gdrive/my-doc-1abc" {
		t.Errorf("conversation = %q, want %q", conversation, "gdrive/my-doc-1abc")
	}
	if date != "Tab 1" {
		t.Errorf("date = %q, want %q", date, "Tab 1")
	}
	if thread {
		t.Error("thread = true, want false")
	}
}

func TestParseFilePath_GWSCalendar(t *testing.T) {
	// 5 segments: gws/account/gcalendar/primary/2026-04-07.jsonl
	platform, account, conversation, date, thread, err := ParseFilePath(
		"/data/gws/user/gcalendar/primary/2026-04-07.jsonl",
		"/data",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if platform != "gws" {
		t.Errorf("platform = %q, want %q", platform, "gws")
	}
	if account != "user" {
		t.Errorf("account = %q, want %q", account, "user")
	}
	if conversation != "gcalendar/primary" {
		t.Errorf("conversation = %q, want %q", conversation, "gcalendar/primary")
	}
	if date != "2026-04-07" {
		t.Errorf("date = %q, want %q", date, "2026-04-07")
	}
	if thread {
		t.Error("thread = true, want false")
	}
}

func TestParseFilePath_GWSGmail(t *testing.T) {
	// 4 segments: gws/account/gmail/2026-04-07.jsonl (standard depth)
	platform, account, conversation, date, _, err := ParseFilePath(
		"/data/gws/user/gmail/2026-04-07.jsonl",
		"/data",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if platform != "gws" {
		t.Errorf("platform = %q, want %q", platform, "gws")
	}
	if account != "user" {
		t.Errorf("account = %q, want %q", account, "user")
	}
	if conversation != "gmail" {
		t.Errorf("conversation = %q, want %q", conversation, "gmail")
	}
	if date != "2026-04-07" {
		t.Errorf("date = %q, want %q", date, "2026-04-07")
	}
}

func TestParseFilePath_GWSComments(t *testing.T) {
	// 5 segments: gws/account/gdrive/doc-slug/comments.jsonl
	platform, _, conversation, date, _, err := ParseFilePath(
		"/data/gws/user/gdrive/my-doc-1abc/comments.jsonl",
		"/data",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if platform != "gws" {
		t.Errorf("platform = %q, want %q", platform, "gws")
	}
	if conversation != "gdrive/my-doc-1abc" {
		t.Errorf("conversation = %q, want %q", conversation, "gdrive/my-doc-1abc")
	}
	if date != "comments" {
		t.Errorf("date = %q, want %q", date, "comments")
	}
}

func TestParseFilePath_GWSCSV(t *testing.T) {
	// 5 segments: gws/account/gdrive/sheet-slug/Sheet1.csv
	_, _, conversation, date, _, err := ParseFilePath(
		"/data/gws/user/gdrive/budget-1xyz/Sheet1.csv",
		"/data",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conversation != "gdrive/budget-1xyz" {
		t.Errorf("conversation = %q, want %q", conversation, "gdrive/budget-1xyz")
	}
	if date != "Sheet1" {
		t.Errorf("date = %q, want %q", date, "Sheet1")
	}
}
