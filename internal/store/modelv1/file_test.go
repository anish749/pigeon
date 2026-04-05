package modelv1

import (
	"testing"
)

// --- ParseDateFile ---

func TestParseDateFile_Mixed(t *testing.T) {
	input := []byte(
		"[2026-03-16 09:15:02 +00:00] [id:M1] [from:U1] Alice: hello\n" +
			"[2026-03-16 09:15:30 +00:00] [id:M2] [from:U2] Bob: world\n" +
			"[2026-03-16 09:16:00 +00:00] [react:M1] [from:U2] Bob: thumbsup\n" +
			"[2026-03-16 09:17:00 +00:00] [edit:M1] [from:U1] Alice: hello updated\n" +
			"[2026-03-16 09:18:00 +00:00] [delete:M2] [from:U2] Bob:\n" +
			"[2026-03-16 09:19:00 +00:00] [unreact:M1] [from:U2] Bob: thumbsup\n",
	)
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
	input := []byte(
		"[2026-03-16 09:15:02 +00:00] [id:M1] [from:U1] Alice: hello\n" +
			"this is garbage\n" +
			"[2026-03-16 09:15:30 +00:00] [id:M2] [from:U2] Bob: world\n",
	)
	f, err := ParseDateFile(input)
	if err != nil {
		t.Fatalf("ParseDateFile: %v", err)
	}
	if len(f.Messages) != 2 {
		t.Errorf("messages = %d, want 2 (garbage line skipped)", len(f.Messages))
	}
}

func TestParseDateFile_MessagesOnly(t *testing.T) {
	input := []byte(
		"[2026-03-16 09:15:02 +00:00] [id:M1] [from:U1] Alice: hello\n" +
			"[2026-03-16 09:15:30 +00:00] [id:M2] [from:U2] Bob: world\n",
	)
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

	data := MarshalDateFile(f)
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
	data := MarshalDateFile(f)
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
	input := []byte(
		"[2026-03-16 09:15:30 +00:00] [id:P1] [from:U1] Bob: starting a thread\n" +
			"  [2026-03-16 09:16:00 +00:00] [id:R1] [from:U2] Alice: replying here\n" +
			"  [2026-03-16 09:17:00 +00:00] [id:R2] [from:U1] Bob: thanks\n" +
			"--- channel context ---\n" +
			"[2026-03-16 09:13:00 +00:00] [id:C1] [from:U3] Charlie: context before\n" +
			"[2026-03-16 09:18:00 +00:00] [id:C2] [from:U3] Charlie: context after\n" +
			"[2026-03-16 09:20:00 +00:00] [react:P1] [from:U2] Alice: tada\n",
	)

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
	input := []byte(
		"[2026-03-16 09:15:30 +00:00] [id:P1] [from:U1] Bob: thread\n" +
			"  [2026-03-16 09:16:00 +00:00] [id:R1] [from:U2] Alice: reply\n",
	)
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

	data := MarshalThreadFile(f)
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

	data := string(MarshalThreadFile(f))
	lines := splitLines([]byte(data))

	// Line 0: parent (not indented)
	if lines[0][:2] == "  " {
		t.Error("parent should not be indented")
	}
	// Line 1: reply (indented)
	if lines[1][:2] != "  " {
		t.Error("reply should be indented")
	}
	// Line 2: separator
	if lines[2] != SeparatorLine {
		t.Errorf("line 2 = %q, want separator", lines[2])
	}
	// Line 3: context
	if lines[3][:2] == "  " {
		t.Error("context should not be indented")
	}
	// Line 4: reaction
	if !contains(lines[4], "[react:") {
		t.Errorf("line 4 should be reaction, got: %s", lines[4])
	}
}

func TestMarshalThreadFile_NoContext_NoSeparator(t *testing.T) {
	f := &ThreadFile{
		Parent:  MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U1", Text: "parent"},
		Replies: []MsgLine{{ID: "R1", Ts: ts(2026, 3, 16, 9, 16, 0), Sender: "Alice", SenderID: "U2", Text: "reply", Reply: true}},
	}

	data := string(MarshalThreadFile(f))
	if contains(data, SeparatorLine) {
		t.Error("should not have separator when no context")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
