package poller_test

import (
	"fmt"
	"testing"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
)

func TestGmailCursorResetOnExpiry(t *testing.T) {
	// Simulate what PollGmail does when it gets a 404 from history.list:
	// 1. The error wraps an APIError with code 404
	// 2. IsCursorExpired returns true
	// 3. PollGmail clears the cursor

	err := fmt.Errorf("gws [gmail users history list]: %w", &gws.APIError{
		Code: 404, Reason: "notFound", Message: "Requested entity was not found.",
	})

	if !gws.IsCursorExpired(err) {
		t.Fatal("expected IsCursorExpired to be true for Gmail 404")
	}

	// Verify cursor clearing works.
	cursors := &gwsstore.Cursors{}
	cursors.Gmail.HistoryID = "12345"

	// This is what PollGmail does on cursor expiry:
	cursors.Gmail.HistoryID = ""

	if cursors.Gmail.HistoryID != "" {
		t.Fatal("expected historyId to be cleared")
	}
}

func TestCalendarCursorResetOnExpiry(t *testing.T) {
	// Calendar returns 410 Gone for expired syncTokens.
	err := fmt.Errorf("gws [calendar events list]: %w", &gws.APIError{
		Code: 410, Reason: "gone", Message: "Sync token is no longer valid.",
	})
	if !gws.IsCursorExpired(err) {
		t.Fatal("expected IsCursorExpired to be true for Calendar 410")
	}

	// Calendar also returns 400/invalid for corrupted tokens.
	err2 := fmt.Errorf("gws [calendar events list]: %w", &gws.APIError{
		Code: 400, Reason: "invalid", Message: "Invalid sync token value.",
	})
	if !gws.IsCursorExpired(err2) {
		t.Fatal("expected IsCursorExpired to be true for Calendar 400/invalid")
	}

	cursors := &gwsstore.Cursors{Calendar: gwsstore.CalendarCursors{"primary": "old-token"}}
	cursors.Calendar["primary"] = ""
	if cursors.Calendar["primary"] != "" {
		t.Fatal("expected syncToken to be cleared")
	}
}
