package model

import (
	"encoding/json"
	"testing"
	"time"

	gcal "google.golang.org/api/calendar/v3"
)

func TestMarshalParseEmail(t *testing.T) {
	ts := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	orig := Line{
		Type: "email",
		Email: &EmailLine{
			ID:       "msg-1",
			ThreadID: "thread-1",
			Ts:       ts,
			From:     "alice@example.com",
			FromName: "Alice",
			To:       []string{"bob@example.com"},
			CC:       []string{"carol@example.com"},
			Subject:  "Hello",
			Labels:   []string{"INBOX"},
			Snippet:  "Hi Bob",
			Text:     "Hi Bob, how are you?",
			Attach: []EmailAttachment{
				{ID: "att-1", Type: "application/pdf", Name: "doc.pdf"},
			},
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.Type != "email" {
		t.Errorf("Type = %q, want %q", got.Type, "email")
	}
	if got.Email == nil {
		t.Fatal("Email is nil")
	}
	e := got.Email
	if e.ID != "msg-1" {
		t.Errorf("ID = %q, want %q", e.ID, "msg-1")
	}
	if e.ThreadID != "thread-1" {
		t.Errorf("ThreadID = %q, want %q", e.ThreadID, "thread-1")
	}
	if !e.Ts.Equal(ts) {
		t.Errorf("Ts = %v, want %v", e.Ts, ts)
	}
	if e.From != "alice@example.com" {
		t.Errorf("From = %q, want %q", e.From, "alice@example.com")
	}
	if e.Subject != "Hello" {
		t.Errorf("Subject = %q, want %q", e.Subject, "Hello")
	}
	if len(e.Attach) != 1 || e.Attach[0].Name != "doc.pdf" {
		t.Errorf("Attach = %v, want 1 attachment named doc.pdf", e.Attach)
	}
}

func TestMarshalParseEmailDelete(t *testing.T) {
	ts := time.Date(2026, 4, 7, 13, 0, 0, 0, time.UTC)
	orig := Line{
		Type: "email-delete",
		EmailDelete: &EmailDeleteLine{
			ID:   "msg-1",
			Ts:   ts,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.Type != "email-delete" {
		t.Errorf("Type = %q, want %q", got.Type, "email-delete")
	}
	if got.EmailDelete == nil {
		t.Fatal("EmailDelete is nil")
	}
	if got.EmailDelete.ID != "msg-1" {
		t.Errorf("ID = %q, want %q", got.EmailDelete.ID, "msg-1")
	}
}

func TestMarshalParseComment(t *testing.T) {
	ts := time.Date(2026, 4, 7, 14, 0, 0, 0, time.UTC)
	orig := Line{
		Type: "comment",
		Comment: &CommentLine{
			ID:       "cmt-1",
			Ts:       ts,
			Author:   "Alice",
			Content:  "Please review this section",
			Anchor:   "highlighted text",
			Resolved: false,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.Type != "comment" {
		t.Errorf("Type = %q, want %q", got.Type, "comment")
	}
	if got.Comment == nil {
		t.Fatal("Comment is nil")
	}
	c := got.Comment
	if c.ID != "cmt-1" {
		t.Errorf("ID = %q, want %q", c.ID, "cmt-1")
	}
	if c.Author != "Alice" {
		t.Errorf("Author = %q, want %q", c.Author, "Alice")
	}
	if c.Anchor != "highlighted text" {
		t.Errorf("Anchor = %q, want %q", c.Anchor, "highlighted text")
	}
}

func TestMarshalParseReply(t *testing.T) {
	ts := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	orig := Line{
		Type: "reply",
		Reply: &ReplyLine{
			ID:        "rpl-1",
			CommentID: "cmt-1",
			Ts:        ts,
			Author:    "Bob",
			Content:   "Done",
			Action:    "resolve",
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.Type != "reply" {
		t.Errorf("Type = %q, want %q", got.Type, "reply")
	}
	if got.Reply == nil {
		t.Fatal("Reply is nil")
	}
	r := got.Reply
	if r.ID != "rpl-1" {
		t.Errorf("ID = %q, want %q", r.ID, "rpl-1")
	}
	if r.CommentID != "cmt-1" {
		t.Errorf("CommentID = %q, want %q", r.CommentID, "cmt-1")
	}
	if r.Action != "resolve" {
		t.Errorf("Action = %q, want %q", r.Action, "resolve")
	}
}

func TestMarshalParseEvent(t *testing.T) {
	// Build a CalendarEvent by marshalling a raw API-shaped JSON and parsing
	// it both ways — mirrors how the calendar client populates Parsed + Raw.
	rawJSON := `{
		"id": "evt-1",
		"created": "2026-04-07T10:00:00Z",
		"updated": "2026-04-07T10:00:00Z",
		"status": "confirmed",
		"summary": "Team standup",
		"description": "Daily team sync",
		"start": {"dateTime": "2026-04-07T09:00:00-07:00"},
		"end": {"dateTime": "2026-04-07T09:30:00-07:00"},
		"location": "Room 42",
		"organizer": {"email": "alice@example.com", "displayName": "Alice"},
		"attendees": [
			{"email": "bob@example.com", "displayName": "Bob", "responseStatus": "accepted"},
			{"email": "carol@example.com", "responseStatus": "needsAction"}
		],
		"hangoutLink": "https://meet.google.com/abc-defg-hij",
		"eventType": "default",
		"recurringEventId": "evt-base",
		"iCalUID": "evt-1@google.com",
		"sequence": 2
	}`
	var parsed gcal.Event
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		t.Fatalf("unmarshal typed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	orig := Line{
		Type:  "event",
		Event: &CalendarEvent{Runtime: &parsed, Serialized: raw},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Parse(string(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.Type != "event" {
		t.Errorf("Type = %q, want %q", got.Type, "event")
	}
	if got.Event == nil {
		t.Fatal("Event is nil")
	}

	// Runtime view round-trips.
	ev := got.Event.Runtime
	if ev.Id != "evt-1" {
		t.Errorf("Runtime.Id = %q, want %q", ev.Id, "evt-1")
	}
	if ev.Summary != "Team standup" {
		t.Errorf("Runtime.Summary = %q, want %q", ev.Summary, "Team standup")
	}
	if ev.Location != "Room 42" {
		t.Errorf("Runtime.Location = %q, want %q", ev.Location, "Room 42")
	}
	if len(ev.Attendees) != 2 {
		t.Errorf("Runtime.Attendees count = %d, want 2", len(ev.Attendees))
	}
	if ev.RecurringEventId != "evt-base" {
		t.Errorf("Runtime.RecurringEventId = %q, want %q", ev.RecurringEventId, "evt-base")
	}
	if ev.Start == nil || ev.Start.DateTime != "2026-04-07T09:00:00-07:00" {
		t.Errorf("Runtime.Start.DateTime = %v, want 2026-04-07T09:00:00-07:00", ev.Start)
	}

	// Serialized view preserves everything, including fields the runtime view
	// doesn't need to read directly.
	if got.Event.Serialized["id"] != "evt-1" {
		t.Errorf("Serialized[id] = %v, want evt-1", got.Event.Serialized["id"])
	}
	if _, hasType := got.Event.Serialized["type"]; hasType {
		t.Error("Serialized should not contain the storage type discriminator")
	}
	// A field the runtime view typically ignores is still preserved — proving
	// the serialized form is lossless.
	if got.Event.Serialized["iCalUID"] != "evt-1@google.com" {
		t.Errorf("Serialized[iCalUID] = %v, want evt-1@google.com", got.Event.Serialized["iCalUID"])
	}
}

func TestParseUnknownType(t *testing.T) {
	_, err := Parse(`{"type":"bogus","id":"x"}`)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	_, err := Parse(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshalNoField(t *testing.T) {
	_, err := Marshal(Line{})
	if err == nil {
		t.Fatal("expected error for empty line")
	}
}

func TestLineID(t *testing.T) {
	tests := []struct {
		name string
		line Line
		want string
	}{
		{"email", Line{Email: &EmailLine{ID: "e1"}}, "e1"},
		{"email-delete", Line{EmailDelete: &EmailDeleteLine{ID: "e1"}}, "e1"},
		{"comment", Line{Comment: &CommentLine{ID: "c1"}}, "c1"},
		{"reply", Line{Reply: &ReplyLine{ID: "r1"}}, "r1"},
		{"event", Line{Event: &CalendarEvent{Runtime: &gcal.Event{Id: "v1"}}}, "v1"},
		{"empty", Line{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.line.LineID(); got != tt.want {
				t.Errorf("LineID() = %q, want %q", got, tt.want)
			}
		})
	}
}
