package modelv1

import (
	"strings"
	"testing"
)

func mustLine(t *testing.T, l Line) string {
	t.Helper()
	data, err := Marshal(l)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(data)
}

// --- ParseDateFile ---

func TestParseDateFile_Mixed(t *testing.T) {
	msg1 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}})
	msg2 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U2", Text: "world"}})
	react := mustLine(t, Line{Type: LineReaction, React: &ReactLine{Ts: ts(2026, 3, 16, 9, 16, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"}})
	edit := mustLine(t, Line{Type: LineEdit, Edit: &EditLine{Ts: ts(2026, 3, 16, 9, 17, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "hello updated"}})
	del := mustLine(t, Line{Type: LineDelete, Delete: &DeleteLine{Ts: ts(2026, 3, 16, 9, 18, 0), MsgID: "M2", Sender: "Bob", SenderID: "U2"}})
	unreact := mustLine(t, Line{Type: LineUnreaction, React: &ReactLine{Ts: ts(2026, 3, 16, 9, 19, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true}})

	input := []byte(strings.Join([]string{msg1, msg2, react, edit, del, unreact}, "\n") + "\n")

	f, err := ParseDateFile(input)
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}
	if len(f.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(f.Messages))
	}
	if len(f.Reactions) != 2 { // react + unreact
		t.Errorf("reactions = %d, want 2", len(f.Reactions))
	}
	if len(f.Edits) != 1 {
		t.Errorf("edits = %d, want 1", len(f.Edits))
	}
	if len(f.Deletes) != 1 {
		t.Errorf("deletes = %d, want 1", len(f.Deletes))
	}
	// Verify unreact has Remove=true
	for _, r := range f.Reactions {
		if r.Emoji == "thumbsup" && r.Ts.Equal(ts(2026, 3, 16, 9, 19, 0)) {
			if !r.Remove {
				t.Error("unreact line should have Remove=true")
			}
		}
	}
}

func TestParseDateFile_Empty(t *testing.T) {
	f, err := ParseDateFile([]byte{})
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}
	if len(f.Messages) != 0 || len(f.Reactions) != 0 || len(f.Edits) != 0 || len(f.Deletes) != 0 {
		t.Errorf("expected empty DateFile, got %+v", f)
	}
}

func TestParseDateFile_SkipsUnparseableLines(t *testing.T) {
	msg1 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}})
	msg2 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U2", Text: "world"}})

	input := []byte(msg1 + "\nthis is garbage\n" + msg2 + "\n")

	f, err := ParseDateFile(input)
	if err == nil {
		t.Error("expected error for garbage line, got nil")
	}
	if len(f.Messages) != 2 {
		t.Errorf("messages = %d, want 2 (garbage line skipped)", len(f.Messages))
	}
}

func TestParseDateFile_MessagesOnly(t *testing.T) {
	msg1 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Alice", SenderID: "U1", Text: "hello"}})
	msg2 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U2", Text: "world"}})

	input := []byte(msg1 + "\n" + msg2 + "\n")

	f, err := ParseDateFile(input)
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}
	if len(f.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(f.Messages))
	}
	if len(f.Reactions) != 0 || len(f.Edits) != 0 || len(f.Deletes) != 0 {
		t.Error("expected no reactions/edits/deletes")
	}
}

// --- MarshalDateFile round-trip ---

func TestDateFile_RoundTrip(t *testing.T) {
	f := &DateFile{
		Messages: []MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
		},
		Reactions: []ReactLine{
			{Ts: ts(2026, 3, 16, 9, 17, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
		Edits: []EditLine{
			{Ts: ts(2026, 3, 16, 9, 18, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "hello updated"},
		},
		Deletes: []DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 19, 0), MsgID: "M2", Sender: "Bob", SenderID: "U2"},
		},
	}

	data, merr := MarshalDateFile(f)
	if merr != nil {
		t.Fatalf("MarshalDateFile: %v", merr)
	}
	parsed, err := ParseDateFile(data)
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}

	if len(parsed.Messages) != len(f.Messages) {
		t.Errorf("messages: got %d, want %d", len(parsed.Messages), len(f.Messages))
	}
	if len(parsed.Reactions) != len(f.Reactions) {
		t.Errorf("reactions: got %d, want %d", len(parsed.Reactions), len(f.Reactions))
	}
	if len(parsed.Edits) != len(f.Edits) {
		t.Errorf("edits: got %d, want %d", len(parsed.Edits), len(f.Edits))
	}
	if len(parsed.Deletes) != len(f.Deletes) {
		t.Errorf("deletes: got %d, want %d", len(parsed.Deletes), len(f.Deletes))
	}

	// Verify chronological order in marshalled output
	for i := 1; i < len(parsed.Messages); i++ {
		if parsed.Messages[i].Ts.Before(parsed.Messages[i-1].Ts) {
			t.Errorf("messages not chronological at index %d", i)
		}
	}
}

func TestMarshalDateFile_ChronologicalOrder(t *testing.T) {
	// Out-of-order input should produce chronological output
	f := &DateFile{
		Messages: []MsgLine{
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Bob", SenderID: "U2", Text: "second"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Alice", SenderID: "U1", Text: "first"},
		},
		Reactions: []ReactLine{
			{Ts: ts(2026, 3, 16, 9, 15, 30), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
	}
	data, merr := MarshalDateFile(f)
	if merr != nil {
		t.Fatalf("MarshalDateFile: %v", merr)
	}
	parsed, err := ParseDateFile(data)
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}
	// First event should be M1 (earliest timestamp)
	if parsed.Messages[0].ID != "M1" {
		t.Errorf("first message ID = %q, want M1", parsed.Messages[0].ID)
	}
}

// --- ParseThreadFile ---

func TestParseThreadFile_FullStructure(t *testing.T) {
	parent := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U1", Text: "starting a thread"}})
	reply1 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "replying here", Reply: true}})
	reply2 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "R2", Ts: ts(2026, 3, 16, 9, 17, 0), Sender: "Bob", SenderID: "U1", Text: "thanks", Reply: true}})
	ctx1 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "C1", Ts: ts(2026, 3, 16, 9, 13, 0), Sender: "Charlie", SenderID: "U3", Text: "context before"}})
	ctx2 := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "C2", Ts: ts(2026, 3, 16, 9, 18, 0), Sender: "Charlie", SenderID: "U3", Text: "context after"}})
	react := mustLine(t, Line{Type: LineReaction, React: &ReactLine{Ts: ts(2026, 3, 16, 9, 20, 0), MsgID: "P1", Sender: "Alice", SenderID: "U2", Emoji: "tada"}})

	input := []byte(strings.Join([]string{parent, reply1, reply2, SeparatorLine, ctx1, ctx2, react}, "\n") + "\n")

	f, err := ParseThreadFile(input)
	if err != nil {
		t.Fatalf("ParseThreadFile: %v", err)
	}

	if f.Parent.ID != "P1" {
		t.Errorf("parent ID = %q, want P1", f.Parent.ID)
	}
	if len(f.Replies) != 2 {
		t.Errorf("replies = %d, want 2", len(f.Replies))
	}
	if len(f.Context) != 2 {
		t.Errorf("context = %d, want 2", len(f.Context))
	}
	if len(f.Reactions) != 1 {
		t.Errorf("reactions = %d, want 1", len(f.Reactions))
	}
	// Replies should have Reply=true
	for _, r := range f.Replies {
		if !r.Reply {
			t.Errorf("reply %q should have Reply=true", r.ID)
		}
	}
}

func TestParseThreadFile_Empty(t *testing.T) {
	f, err := ParseThreadFile([]byte{})
	if err != nil {
		t.Fatalf("ParseThreadFile: %v", err)
	}
	if f.Parent.ID != "" {
		t.Errorf("expected empty parent, got ID=%q", f.Parent.ID)
	}
}

func TestParseThreadFile_NoContext(t *testing.T) {
	parent := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U1", Text: "thread"}})
	reply := mustLine(t, Line{Type: LineMessage, Msg: &MsgLine{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "reply", Reply: true}})

	input := []byte(parent + "\n" + reply + "\n")

	f, err := ParseThreadFile(input)
	if err != nil {
		t.Fatalf("ParseThreadFile: %v", err)
	}
	if f.Parent.ID != "P1" {
		t.Errorf("parent ID = %q, want P1", f.Parent.ID)
	}
	if len(f.Replies) != 1 {
		t.Errorf("replies = %d, want 1", len(f.Replies))
	}
	if len(f.Context) != 0 {
		t.Errorf("context = %d, want 0", len(f.Context))
	}
}

// --- MarshalThreadFile round-trip ---

func TestThreadFile_RoundTrip(t *testing.T) {
	f := &ThreadFile{
		Parent: MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 30), Sender: "Bob", SenderID: "U1", Text: "starting a thread"},
		Replies: []MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "replying here", Reply: true},
			{ID: "R2", Ts: ts(2026, 3, 16, 9, 17, 0), Sender: "Bob", SenderID: "U1", Text: "thanks", Reply: true},
		},
		Context: []MsgLine{
			{ID: "C1", Ts: ts(2026, 3, 16, 9, 13, 0), Sender: "Charlie", SenderID: "U3", Text: "context before"},
			{ID: "C2", Ts: ts(2026, 3, 16, 9, 18, 0), Sender: "Charlie", SenderID: "U3", Text: "context after"},
		},
		Reactions: []ReactLine{
			{Ts: ts(2026, 3, 16, 9, 20, 0), MsgID: "P1", Sender: "Alice", SenderID: "U2", Emoji: "tada"},
		},
	}

	data, merr := MarshalThreadFile(f)
	if merr != nil {
		t.Fatalf("MarshalThreadFile: %v", merr)
	}
	parsed, err := ParseThreadFile(data)
	if err != nil {
		t.Fatalf("ParseThreadFile: %v", err)
	}

	if parsed.Parent.ID != f.Parent.ID {
		t.Errorf("parent ID: got %q, want %q", parsed.Parent.ID, f.Parent.ID)
	}
	if len(parsed.Replies) != len(f.Replies) {
		t.Errorf("replies: got %d, want %d", len(parsed.Replies), len(f.Replies))
	}
	if len(parsed.Context) != len(f.Context) {
		t.Errorf("context: got %d, want %d", len(parsed.Context), len(f.Context))
	}
	if len(parsed.Reactions) != len(f.Reactions) {
		t.Errorf("reactions: got %d, want %d", len(parsed.Reactions), len(f.Reactions))
	}

	// Verify replies have Reply=true
	for i, r := range parsed.Replies {
		if !r.Reply {
			t.Errorf("parsed reply[%d] should have Reply=true", i)
		}
	}
}

func TestMarshalThreadFile_SectionOrder(t *testing.T) {
	f := &ThreadFile{
		Parent:  MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U1", Text: "parent"},
		Replies: []MsgLine{{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "reply", Reply: true}},
		Context: []MsgLine{{ID: "C1", Ts: ts(2026, 3, 16, 9, 13, 0), Sender: "Charlie", SenderID: "U3", Text: "context"}},
		Reactions: []ReactLine{{Ts: ts(2026, 3, 16, 9, 20, 0), MsgID: "P1", Sender: "Alice", SenderID: "U2", Emoji: "tada"}},
	}

	rawData, merr := MarshalThreadFile(f)
	if merr != nil {
		t.Fatalf("MarshalThreadFile: %v", merr)
	}
	lines := splitLines(rawData)

	// Line 0: parent (no reply field)
	if strings.Contains(lines[0], `"reply":true`) {
		t.Error("parent should not have reply=true")
	}
	// Line 1: reply (has reply field)
	if !strings.Contains(lines[1], `"reply":true`) {
		t.Error("reply should have reply=true")
	}
	// Line 2: separator
	if lines[2] != SeparatorLine {
		t.Errorf("line 2 = %q, want separator", lines[2])
	}
	// Line 3: context
	if strings.Contains(lines[3], `"reply":true`) {
		t.Error("context should not have reply=true")
	}
	// Line 4: reaction
	if !strings.Contains(lines[4], `"type":"react"`) {
		t.Errorf("line 4 should be reaction, got: %s", lines[4])
	}
}

func TestMarshalThreadFile_NoContext_NoSeparator(t *testing.T) {
	f := &ThreadFile{
		Parent:  MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U1", Text: "parent"},
		Replies: []MsgLine{{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "reply", Reply: true}},
	}

	rawData, merr := MarshalThreadFile(f)
	if merr != nil {
		t.Fatalf("MarshalThreadFile: %v", merr)
	}
	data := string(rawData)
	if strings.Contains(data, SeparatorLine) {
		t.Error("should not have separator when no context")
	}
}
