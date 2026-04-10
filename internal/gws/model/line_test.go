package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gcal "google.golang.org/api/calendar/v3"
	drive "google.golang.org/api/drive/v3"
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

func TestMarshalParseComment(t *testing.T) {
	// Build a DriveComment by unmarshalling a raw API-shaped JSON both ways —
	// mirrors how drive.ListComments populates Runtime + Serialized. Replies
	// are nested inside the comment, just like the API returns them.
	rawJSON := `{
		"id": "cmt-1",
		"author": {"displayName": "Alice", "me": false, "kind": "drive#user"},
		"content": "Please review this section",
		"htmlContent": "<p>Please review this section</p>",
		"quotedFileContent": {"value": "highlighted text", "mimeType": "text/plain"},
		"resolved": false,
		"createdTime": "2026-04-07T14:00:00Z",
		"modifiedTime": "2026-04-07T14:05:00Z",
		"replies": [
			{
				"id": "rpl-1",
				"author": {"displayName": "Bob"},
				"content": "Done",
				"createdTime": "2026-04-07T15:00:00Z",
				"action": "resolve"
			}
		]
	}`
	var runtime drive.Comment
	if err := json.Unmarshal([]byte(rawJSON), &runtime); err != nil {
		t.Fatalf("unmarshal typed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	orig := Line{
		Type:    "comment",
		Comment: &DriveComment{Runtime: runtime, Serialized: raw},
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

	// Runtime view round-trips, including nested replies.
	c := got.Comment.Runtime
	if c.Id != "cmt-1" {
		t.Errorf("Runtime.Id = %q, want %q", c.Id, "cmt-1")
	}
	if c.Author == nil || c.Author.DisplayName != "Alice" {
		t.Errorf("Runtime.Author = %v, want Alice", c.Author)
	}
	if c.QuotedFileContent == nil || c.QuotedFileContent.Value != "highlighted text" {
		t.Errorf("Runtime.QuotedFileContent = %v, want highlighted text", c.QuotedFileContent)
	}
	if len(c.Replies) != 1 {
		t.Fatalf("Runtime.Replies count = %d, want 1", len(c.Replies))
	}
	if c.Replies[0].Id != "rpl-1" || c.Replies[0].Action != "resolve" {
		t.Errorf("Runtime.Replies[0] = %+v, want rpl-1/resolve", c.Replies[0])
	}

	// Serialized view preserves everything — including fields the runtime view
	// may not pluck out (htmlContent, kind) — proving storage is lossless.
	if got.Comment.Serialized["id"] != "cmt-1" {
		t.Errorf("Serialized[id] = %v, want cmt-1", got.Comment.Serialized["id"])
	}
	if _, hasType := got.Comment.Serialized["type"]; hasType {
		t.Error("Serialized should not contain the storage type discriminator")
	}
	if got.Comment.Serialized["htmlContent"] != "<p>Please review this section</p>" {
		t.Errorf("Serialized[htmlContent] = %v, want <p>...</p>", got.Comment.Serialized["htmlContent"])
	}
	// Replies preserved nested inside the raw comment.
	rawReplies, ok := got.Comment.Serialized["replies"].([]any)
	if !ok || len(rawReplies) != 1 {
		t.Fatalf("Serialized[replies] = %v, want 1-element array", got.Comment.Serialized["replies"])
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
		Event: &CalendarEvent{Runtime: parsed, Serialized: raw},
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
		{"comment", Line{Comment: &DriveComment{Runtime: drive.Comment{Id: "c1"}}}, "c1"},
		{"event", Line{Event: &CalendarEvent{Runtime: gcal.Event{Id: "v1"}}}, "v1"},
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

func TestMarshalRaw_InjectsTypeDiscriminator(t *testing.T) {
	in := map[string]any{"id": "x1", "name": "hello"}
	out, err := marshalRaw(in, "widget")
	if err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got["type"] != "widget" {
		t.Errorf("type = %v, want widget", got["type"])
	}
	if got["id"] != "x1" {
		t.Errorf("id = %v, want x1", got["id"])
	}
	if got["name"] != "hello" {
		t.Errorf("name = %v, want hello", got["name"])
	}
}

func TestMarshalRaw_DoesNotMutateCaller(t *testing.T) {
	in := map[string]any{"id": "x1"}
	if _, err := marshalRaw(in, "widget"); err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}
	if _, hasType := in["type"]; hasType {
		t.Error("caller's map was mutated: type key was injected")
	}
	if len(in) != 1 {
		t.Errorf("caller's map size = %d, want 1", len(in))
	}
}

func TestMarshalRaw_EmptyMap(t *testing.T) {
	out, err := marshalRaw(map[string]any{}, "widget")
	if err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}
	if string(out) != `{"type":"widget"}` {
		t.Errorf("got %s, want {\"type\":\"widget\"}", out)
	}
}

func TestMarshalRaw_NilMap(t *testing.T) {
	out, err := marshalRaw(nil, "widget")
	if err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}
	if string(out) != `{"type":"widget"}` {
		t.Errorf("got %s, want {\"type\":\"widget\"}", out)
	}
}

func TestMarshalRaw_OverwritesExistingTypeKey(t *testing.T) {
	// If the caller's map already has a "type" key (e.g. from an API response
	// that happened to use that field name), marshalRaw should overwrite it
	// with the storage discriminator. The caller's map must not be mutated.
	in := map[string]any{"type": "api-provided", "id": "x1"}
	out, err := marshalRaw(in, "widget")
	if err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got["type"] != "widget" {
		t.Errorf("type = %v, want widget (discriminator should win)", got["type"])
	}
	if in["type"] != "api-provided" {
		t.Errorf("caller's map was mutated: in[type] = %v, want api-provided", in["type"])
	}
}

// testItem is a minimal typed struct used by unmarshalRaw tests.
type testItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestUnmarshalRaw_PopulatesTypedAndStripsType(t *testing.T) {
	data := []byte(`{"type":"widget","id":"x1","name":"hello","extra":42}`)
	var runtime testItem
	serialized, err := unmarshalRaw(data, &runtime)
	if err != nil {
		t.Fatalf("unmarshalRaw: %v", err)
	}

	// Typed view picks up known fields.
	if runtime.ID != "x1" {
		t.Errorf("runtime.ID = %q, want x1", runtime.ID)
	}
	if runtime.Name != "hello" {
		t.Errorf("runtime.Name = %q, want hello", runtime.Name)
	}

	// Serialized drops the storage discriminator but keeps everything else,
	// including fields the typed view doesn't know about.
	if _, hasType := serialized["type"]; hasType {
		t.Error("serialized should not contain the type discriminator")
	}
	if serialized["id"] != "x1" {
		t.Errorf("serialized[id] = %v, want x1", serialized["id"])
	}
	if serialized["extra"] != float64(42) {
		t.Errorf("serialized[extra] = %v, want 42 (proves unknown fields are preserved)", serialized["extra"])
	}
}

func TestUnmarshalRaw_NoTypeKey(t *testing.T) {
	// Input without a "type" key should still work — unmarshalRaw is
	// tolerant of missing discriminators (the delete is a no-op).
	data := []byte(`{"id":"x1","name":"hello"}`)
	var runtime testItem
	serialized, err := unmarshalRaw(data, &runtime)
	if err != nil {
		t.Fatalf("unmarshalRaw: %v", err)
	}
	if runtime.ID != "x1" {
		t.Errorf("runtime.ID = %q, want x1", runtime.ID)
	}
	if serialized["id"] != "x1" {
		t.Errorf("serialized[id] = %v, want x1", serialized["id"])
	}
}

func TestUnmarshalRaw_InvalidJSON(t *testing.T) {
	var runtime testItem
	_, err := unmarshalRaw([]byte(`not json`), &runtime)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshalUnmarshalRaw_RoundTrip(t *testing.T) {
	// Round trip: serialized map → marshalRaw → bytes → unmarshalRaw →
	// serialized map should equal the original, and the typed view should
	// pick up its fields correctly.
	orig := map[string]any{
		"id":    "x1",
		"name":  "hello",
		"nested": map[string]any{
			"a": "one",
			"b": float64(2),
		},
		"list": []any{"a", "b", "c"},
		"flag": true,
	}

	data, err := marshalRaw(orig, "widget")
	if err != nil {
		t.Fatalf("marshalRaw: %v", err)
	}

	var runtime testItem
	got, err := unmarshalRaw(data, &runtime)
	if err != nil {
		t.Fatalf("unmarshalRaw: %v", err)
	}

	// Typed view picks up the declared fields.
	if runtime.ID != "x1" || runtime.Name != "hello" {
		t.Errorf("runtime = %+v, want {x1 hello}", runtime)
	}

	// Serialized round-trips exactly (minus the injected type discriminator,
	// which unmarshalRaw strips).
	if diff := cmp.Diff(orig, got); diff != "" {
		t.Errorf("round trip did not preserve serialized map (-orig +got):\n%s", diff)
	}
	if _, hasType := got["type"]; hasType {
		t.Error("round trip left the type discriminator in serialized")
	}
}
