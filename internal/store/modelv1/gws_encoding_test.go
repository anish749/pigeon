package modelv1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gcal "google.golang.org/api/calendar/v3"
	drive "google.golang.org/api/drive/v3"
)

func TestMarshalParseEmail(t *testing.T) {
	at := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	orig := Line{
		Type: LineEmail,
		Email: &EmailLine{
			ID:       "msg-1",
			ThreadID: "thread-1",
			Ts:       at,
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

	if got.Type != LineEmail {
		t.Errorf("Type = %q, want %q", got.Type, LineEmail)
	}
	if got.Email == nil {
		t.Fatal("Email is nil")
	}
	e := got.Email
	if e.ID != "msg-1" {
		t.Errorf("ID = %q, want %q", e.ID, "msg-1")
	}
	if !e.Ts.Equal(at) {
		t.Errorf("Ts = %v, want %v", e.Ts, at)
	}
	if len(e.Attach) != 1 || e.Attach[0].Name != "doc.pdf" {
		t.Errorf("Attach = %v, want 1 attachment named doc.pdf", e.Attach)
	}

	// Ts() on the Line surfaces the email's timestamp.
	if !got.Ts().Equal(at) {
		t.Errorf("Line.Ts() = %v, want %v", got.Ts(), at)
	}
	if id, _ := got.ID(); id != "msg-1" {
		id2, _ := got.ID(); t.Errorf("Line.ID() = %q, want msg-1", id2)
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
		Type:    LineComment,
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

	if got.Type != LineComment {
		t.Errorf("Type = %q, want %q", got.Type, LineComment)
	}
	if got.Comment == nil {
		t.Fatal("Comment is nil")
	}
	c := got.Comment.Runtime
	if c.Id != "cmt-1" {
		t.Errorf("Runtime.Id = %q, want %q", c.Id, "cmt-1")
	}
	if c.Author == nil || c.Author.DisplayName != "Alice" {
		t.Errorf("Runtime.Author = %v, want Alice", c.Author)
	}
	if len(c.Replies) != 1 {
		t.Fatalf("Runtime.Replies count = %d, want 1", len(c.Replies))
	}
	if c.Replies[0].Id != "rpl-1" || c.Replies[0].Action != "resolve" {
		t.Errorf("Runtime.Replies[0] = %+v, want rpl-1/resolve", c.Replies[0])
	}
	// Serialized view preserves everything, including fields the typed view
	// doesn't pluck out — proving storage is lossless.
	if _, hasType := got.Comment.Serialized["type"]; hasType {
		t.Error("Serialized should not contain the storage type discriminator")
	}
	if got.Comment.Serialized["htmlContent"] != "<p>Please review this section</p>" {
		t.Errorf("Serialized[htmlContent] = %v", got.Comment.Serialized["htmlContent"])
	}
	// Line.ID surfaces the drive comment ID.
	if id, _ := got.ID(); id != "cmt-1" {
		id2, _ := got.ID(); t.Errorf("Line.ID() = %q, want cmt-1", id2)
	}
}

func TestMarshalParseEvent(t *testing.T) {
	rawJSON := `{
		"id": "evt-1",
		"created": "2026-04-07T10:00:00Z",
		"updated": "2026-04-07T10:00:00Z",
		"status": "confirmed",
		"summary": "Team standup",
		"start": {"dateTime": "2026-04-07T09:00:00-07:00"},
		"end": {"dateTime": "2026-04-07T09:30:00-07:00"},
		"location": "Room 42",
		"iCalUID": "evt-1@google.com"
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
		Type:  LineEvent,
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

	if got.Type != LineEvent {
		t.Errorf("Type = %q, want %q", got.Type, LineEvent)
	}
	if got.Event == nil {
		t.Fatal("Event is nil")
	}
	ev := got.Event.Runtime
	if ev.Id != "evt-1" {
		t.Errorf("Runtime.Id = %q, want evt-1", ev.Id)
	}
	if ev.Summary != "Team standup" {
		t.Errorf("Runtime.Summary = %q, want Team standup", ev.Summary)
	}
	// A field the runtime view typically ignores is still preserved.
	if got.Event.Serialized["iCalUID"] != "evt-1@google.com" {
		t.Errorf("Serialized[iCalUID] = %v", got.Event.Serialized["iCalUID"])
	}
	if id, _ := got.ID(); id != "evt-1" {
		id2, _ := got.ID(); t.Errorf("Line.ID() = %q, want evt-1", id2)
	}
}

// --- Parse/Marshal error paths ---

func TestParseGWS_UnknownType(t *testing.T) {
	_, err := Parse(`{"type":"bogus","id":"x"}`)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseGWS_InvalidJSON(t *testing.T) {
	_, err := Parse(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshalGWS_NoType(t *testing.T) {
	_, err := Marshal(Line{})
	if err == nil {
		t.Fatal("expected error for empty line with no type")
	}
}

// --- marshalRaw edge cases ---

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

func TestUnmarshalRaw_NoTypeKey(t *testing.T) {
	// Input without a "type" key should still work — unmarshalRaw is
	// tolerant of missing discriminators (the delete is a no-op).
	data := []byte(`{"id":"x1","name":"hello"}`)
	var runtime struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
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

func TestMarshalUnmarshalRaw_RoundTrip(t *testing.T) {
	orig := map[string]any{
		"id":   "x1",
		"name": "hello",
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

	var runtime struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	got, err := unmarshalRaw(data, &runtime)
	if err != nil {
		t.Fatalf("unmarshalRaw: %v", err)
	}
	if runtime.ID != "x1" || runtime.Name != "hello" {
		t.Errorf("runtime = %+v, want {x1 hello}", runtime)
	}
	if diff := cmp.Diff(orig, got); diff != "" {
		t.Errorf("round trip did not preserve serialized map (-orig +got):\n%s", diff)
	}
	if _, hasType := got["type"]; hasType {
		t.Error("round trip left the type discriminator in serialized")
	}
}
