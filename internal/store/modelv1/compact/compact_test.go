package compact_test

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
)

func ts(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

// --- Dedup tests ---

func TestCompact_DedupMessages_KeepsFirst(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "first"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "duplicate"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "hello"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Text != "first" {
		t.Errorf("messages[0].Text = %q, want %q", got.Messages[0].Text, "first")
	}
	if got.Messages[1].ID != "M2" {
		t.Errorf("messages[1].ID = %q, want %q", got.Messages[1].ID, "M2")
	}
}

func TestCompact_DedupReactions_KeepsFirst(t *testing.T) {
	// True duplicates: same event appended twice (identical timestamps).
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if !got.Reactions[0].Ts.Equal(ts(2026, 3, 16, 9, 1, 0)) {
		t.Errorf("kept wrong reaction: ts = %v", got.Reactions[0].Ts)
	}
}

func TestCompact_DedupMessages_PreservesRaw(t *testing.T) {
	raw := map[string]any{
		"files": []any{
			map[string]any{"name": "doc.pdf", "size": float64(999)},
		},
	}
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "", Raw: raw},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "", Raw: raw},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Raw == nil {
		t.Fatal("raw is nil after dedup, want preserved")
	}
	files, ok := got.Messages[0].Raw["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("raw[files] = %v, want slice of 1", got.Messages[0].Raw["files"])
	}
	if files[0].(map[string]any)["name"] != "doc.pdf" {
		t.Errorf("file name = %v, want doc.pdf", files[0].(map[string]any)["name"])
	}
}

func TestCompact_NoDuplicates_Unchanged(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(got.Messages))
	}
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
}

// --- React/unreact reconciliation tests ---

func TestCompact_ReactThenUnreact_BothRemoved(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 0 {
		t.Fatalf("reactions count = %d, want 0", len(got.Reactions))
	}
}

func TestCompact_ReactUnreactReact_FinalReactSurvives(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].Remove {
		t.Error("surviving reaction should not be Remove=true")
	}
	if !got.Reactions[0].Ts.Equal(ts(2026, 3, 16, 9, 3, 0)) {
		t.Errorf("surviving reaction ts = %v, want 09:03", got.Reactions[0].Ts)
	}
}

func TestCompact_UnreactWithNoPriorReact_Removed(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 0 {
		t.Fatalf("reactions count = %d, want 0", len(got.Reactions))
	}
}

func TestCompact_MultipleEmojisSameUserSameMsg_Independent(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "heart"},
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].Emoji != "heart" {
		t.Errorf("surviving emoji = %q, want %q", got.Reactions[0].Emoji, "heart")
	}
}

func TestCompact_MultipleUsersSameEmoji_Independent(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Charlie", SenderID: "U3", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].SenderID != "U3" {
		t.Errorf("surviving reaction sender = %q, want %q", got.Reactions[0].SenderID, "U3")
	}
}

// --- Edit tests ---

func TestCompact_SingleEdit_ReplacesText(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Text != "edited" {
		t.Errorf("text = %q, want %q", got.Messages[0].Text, "edited")
	}
	if len(got.Edits) != 0 {
		t.Errorf("edits should be empty after compaction, got %d", len(got.Edits))
	}
}

func TestCompact_MultipleEdits_MostRecentWins(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "first edit"},
			{Ts: ts(2026, 3, 16, 9, 10, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "second edit"},
			{Ts: ts(2026, 3, 16, 9, 7, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "middle edit"},
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].Text != "second edit" {
		t.Errorf("text = %q, want %q", got.Messages[0].Text, "second edit")
	}
}

func TestCompact_EditWithAttachments_ReplacesAttachments(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{
				ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1",
				Text:        "original",
				Attachments: []modelv1.Attachment{{ID: "F1", Type: "image/jpeg"}},
			},
		},
		Edits: []modelv1.EditLine{
			{
				Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1",
				Text:        "edited",
				Attachments: []modelv1.Attachment{{ID: "F2", Type: "image/png"}, {ID: "F3", Type: "application/pdf"}},
			},
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].Text != "edited" {
		t.Errorf("text = %q, want %q", got.Messages[0].Text, "edited")
	}
	if len(got.Messages[0].Attachments) != 2 {
		t.Fatalf("attachments count = %d, want 2", len(got.Messages[0].Attachments))
	}
	if got.Messages[0].Attachments[0].ID != "F2" {
		t.Errorf("attachment[0].ID = %q, want %q", got.Messages[0].Attachments[0].ID, "F2")
	}
	if got.Messages[0].Attachments[1].ID != "F3" {
		t.Errorf("attachment[1].ID = %q, want %q", got.Messages[0].Attachments[1].ID, "F3")
	}
}

func TestCompact_EditRemovesAttachments(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{
				ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1",
				Text:        "with attachment",
				Attachments: []modelv1.Attachment{{ID: "F1", Type: "image/jpeg"}},
			},
		},
		Edits: []modelv1.EditLine{
			{
				Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1",
				Text: "no attachment",
				// nil Attachments = attachment removed
			},
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].Attachments != nil {
		t.Errorf("attachments should be nil after edit removes them, got %v", got.Messages[0].Attachments)
	}
}

func TestCompact_EditNonexistentMessage_NoError(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "NONEXISTENT", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Text != "hello" {
		t.Errorf("text = %q, want %q", got.Messages[0].Text, "hello")
	}
}

// --- Delete tests ---

func TestCompact_Delete_RemovesMessageAndDeleteLine(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].ID != "M2" {
		t.Errorf("surviving message ID = %q, want %q", got.Messages[0].ID, "M2")
	}
	if len(got.Deletes) != 0 {
		t.Errorf("deletes should be empty after compaction, got %d", len(got.Deletes))
	}
}

func TestCompact_Delete_RemovesAssociatedReactions(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M2", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].MsgID != "M2" {
		t.Errorf("surviving reaction targets %q, want %q", got.Reactions[0].MsgID, "M2")
	}
}

func TestCompact_Delete_RemovesAssociatedEdits(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 0 {
		t.Fatalf("messages count = %d, want 0", len(got.Messages))
	}
	if len(got.Edits) != 0 {
		t.Errorf("edits should be empty after compaction, got %d", len(got.Edits))
	}
}

func TestCompact_DeleteNonexistentMessage_NoError(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "NONEXISTENT", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got.Messages))
	}
	if len(got.Deletes) != 0 {
		t.Errorf("deletes should be empty after compaction, got %d", len(got.Deletes))
	}
}

// --- Sort tests ---

func TestCompact_SortsByTimestamp(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M3", Ts: ts(2026, 3, 16, 9, 30, 0), Sender: "Charlie", SenderID: "U3", Text: "third"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "first"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U2", Text: "second"},
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].ID != "M1" || got.Messages[1].ID != "M2" || got.Messages[2].ID != "M3" {
		t.Errorf("messages not sorted: %v, %v, %v",
			got.Messages[0].ID, got.Messages[1].ID, got.Messages[2].ID)
	}
}

func TestCompact_StableSortPreservesOrder(t *testing.T) {
	sameTs := ts(2026, 3, 16, 9, 0, 0)
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: sameTs, Sender: "Alice", SenderID: "U1", Text: "first"},
			{ID: "M2", Ts: sameTs, Sender: "Bob", SenderID: "U2", Text: "second"},
			{ID: "M3", Ts: sameTs, Sender: "Charlie", SenderID: "U3", Text: "third"},
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].ID != "M1" || got.Messages[1].ID != "M2" || got.Messages[2].ID != "M3" {
		t.Errorf("stable sort not preserved: %v, %v, %v",
			got.Messages[0].ID, got.Messages[1].ID, got.Messages[2].ID)
	}
}

// --- Integration tests ---

func TestCompact_AllEventTypesMixed(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M3", Ts: ts(2026, 3, 16, 9, 30, 0), Sender: "Charlie", SenderID: "U3", Text: "will be deleted"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original text"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "duplicate"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U2", Text: "hello"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
			{Ts: ts(2026, 3, 16, 9, 16, 0), MsgID: "M2", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
			{Ts: ts(2026, 3, 16, 9, 31, 0), MsgID: "M3", Sender: "Bob", SenderID: "U2", Emoji: "wave"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited text"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 35, 0), MsgID: "M3", Sender: "Charlie", SenderID: "U3"},
		},
	}

	got := compact.Compact(f)

	// M1 (deduped, edited) and M2 survive. M3 deleted.
	if len(got.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(got.Messages))
	}

	// Sorted by timestamp: M1 at 09:00, M2 at 09:15.
	if got.Messages[0].ID != "M1" || got.Messages[1].ID != "M2" {
		t.Errorf("message order: %v, %v", got.Messages[0].ID, got.Messages[1].ID)
	}

	// M1 text is edited.
	if got.Messages[0].Text != "edited text" {
		t.Errorf("M1 text = %q, want %q", got.Messages[0].Text, "edited text")
	}

	// Reactions: thumbsup on M1 cancelled (react+unreact), wave on M3 deleted with M3.
	// Only heart on M2 survives.
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].MsgID != "M2" || got.Reactions[0].Emoji != "heart" {
		t.Errorf("surviving reaction: msg=%q emoji=%q", got.Reactions[0].MsgID, got.Reactions[0].Emoji)
	}

	// Edit and delete lines are consumed.
	if len(got.Edits) != 0 {
		t.Errorf("edits should be empty, got %d", len(got.Edits))
	}
	if len(got.Deletes) != 0 {
		t.Errorf("deletes should be empty, got %d", len(got.Deletes))
	}
}

func TestCompact_Idempotent(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U2", Text: "hello"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
	}

	first := compact.Compact(f)
	second := compact.Compact(first)

	if len(first.Messages) != len(second.Messages) {
		t.Fatalf("idempotent check: messages count %d vs %d", len(first.Messages), len(second.Messages))
	}
	for i := range first.Messages {
		if first.Messages[i].ID != second.Messages[i].ID {
			t.Errorf("idempotent check: message[%d] ID %q vs %q", i, first.Messages[i].ID, second.Messages[i].ID)
		}
		if first.Messages[i].Text != second.Messages[i].Text {
			t.Errorf("idempotent check: message[%d] text %q vs %q", i, first.Messages[i].Text, second.Messages[i].Text)
		}
	}
	if len(first.Reactions) != len(second.Reactions) {
		t.Fatalf("idempotent check: reactions count %d vs %d", len(first.Reactions), len(second.Reactions))
	}
}

func TestCompact_EmptyDateFile(t *testing.T) {
	got := compact.Compact(&modelv1.DateFile{})
	if len(got.Messages) != 0 || len(got.Reactions) != 0 || len(got.Edits) != 0 || len(got.Deletes) != 0 {
		t.Errorf("compact of empty file should be empty, got messages=%d reactions=%d edits=%d deletes=%d",
			len(got.Messages), len(got.Reactions), len(got.Edits), len(got.Deletes))
	}
}

func TestCompact_NilDateFile(t *testing.T) {
	got := compact.Compact(nil)
	if got == nil {
		t.Fatal("compact of nil should return non-nil empty modelv1.DateFile")
	}
	if len(got.Messages) != 0 {
		t.Errorf("messages count = %d, want 0", len(got.Messages))
	}
}

func TestCompact_DoesNotMutateInput(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 15, 0), Sender: "Bob", SenderID: "U2", Text: "second"},
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "first"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
	}

	// Save original state.
	origFirstID := f.Messages[0].ID
	origFirstText := f.Messages[0].Text
	origSecondText := f.Messages[1].Text

	_ = compact.Compact(f)

	// Verify input not mutated.
	if f.Messages[0].ID != origFirstID {
		t.Error("input Messages order was mutated")
	}
	if f.Messages[0].Text != origFirstText {
		t.Errorf("input Messages[0].Text mutated: %q", f.Messages[0].Text)
	}
	if f.Messages[1].Text != origSecondText {
		t.Errorf("input Messages[1].Text mutated: %q", f.Messages[1].Text)
	}
	if len(f.Edits) != 1 {
		t.Error("input Edits was mutated")
	}
}

// --- CompactThread tests ---

func TestCompactThread_Nil(t *testing.T) {
	got := compact.CompactThread(nil)
	if got != nil {
		t.Error("compact.CompactThread(nil) should return nil")
	}
}

func TestCompactThread_DeletedParent_ReturnsNil(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "P1", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.CompactThread(f)
	if got != nil {
		t.Error("CompactThread with deleted parent should return nil")
	}
}

func TestCompactThread_EditParent(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "P1", Sender: "Alice", SenderID: "U1", Text: "edited parent"},
		},
	}
	got := compact.CompactThread(f)
	if got.Parent.Text != "edited parent" {
		t.Errorf("parent text = %q, want %q", got.Parent.Text, "edited parent")
	}
	if len(got.Replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(got.Replies))
	}
}

func TestCompactThread_EditReply(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "original reply", Reply: true},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "R1", Sender: "Bob", SenderID: "U2", Text: "edited reply"},
		},
	}
	got := compact.CompactThread(f)
	if got.Replies[0].Text != "edited reply" {
		t.Errorf("reply text = %q, want %q", got.Replies[0].Text, "edited reply")
	}
}

func TestCompactThread_EditContext(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Context: []modelv1.MsgLine{
			{ID: "C1", Ts: ts(2026, 3, 16, 8, 55, 0), Sender: "Charlie", SenderID: "U3", Text: "context msg"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "C1", Sender: "Charlie", SenderID: "U3", Text: "edited context"},
		},
	}
	got := compact.CompactThread(f)
	if got.Context[0].Text != "edited context" {
		t.Errorf("context text = %q, want %q", got.Context[0].Text, "edited context")
	}
}

func TestCompactThread_DeleteReply(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply1", Reply: true},
			{ID: "R2", Ts: ts(2026, 3, 16, 9, 2, 0), Sender: "Charlie", SenderID: "U3", Text: "reply2", Reply: true},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "R1", Sender: "Bob", SenderID: "U2"},
		},
	}
	got := compact.CompactThread(f)
	if len(got.Replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(got.Replies))
	}
	if got.Replies[0].ID != "R2" {
		t.Errorf("surviving reply ID = %q, want %q", got.Replies[0].ID, "R2")
	}
}

func TestCompactThread_DeleteContext(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Context: []modelv1.MsgLine{
			{ID: "C1", Ts: ts(2026, 3, 16, 8, 55, 0), Sender: "Charlie", SenderID: "U3", Text: "context before"},
			{ID: "C2", Ts: ts(2026, 3, 16, 9, 5, 0), Sender: "Dave", SenderID: "U4", Text: "context after"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 10, 0), MsgID: "C1", Sender: "Charlie", SenderID: "U3"},
		},
	}
	got := compact.CompactThread(f)
	if len(got.Context) != 1 {
		t.Fatalf("context count = %d, want 1", len(got.Context))
	}
	if got.Context[0].ID != "C2" {
		t.Errorf("surviving context ID = %q, want %q", got.Context[0].ID, "C2")
	}
}

func TestCompactThread_ReactionsOnParentAndReplies(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "P1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "R1", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
			{Ts: ts(2026, 3, 16, 9, 4, 0), MsgID: "P1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		},
	}
	got := compact.CompactThread(f)
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].MsgID != "R1" || got.Reactions[0].Emoji != "heart" {
		t.Errorf("surviving reaction: msg=%q emoji=%q", got.Reactions[0].MsgID, got.Reactions[0].Emoji)
	}
}

func TestCompactThread_PreservesReplyFlag(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true},
		},
		Context: []modelv1.MsgLine{
			{ID: "C1", Ts: ts(2026, 3, 16, 8, 55, 0), Sender: "Charlie", SenderID: "U3", Text: "context"},
		},
	}
	got := compact.CompactThread(f)
	if got.Parent.Reply {
		t.Error("parent should not have Reply=true")
	}
	if !got.Replies[0].Reply {
		t.Error("reply should have Reply=true")
	}
	if got.Context[0].Reply {
		t.Error("context should not have Reply=true")
	}
}

func TestCompactThread_SortsRepliesAndContext(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R2", Ts: ts(2026, 3, 16, 9, 3, 0), Sender: "Charlie", SenderID: "U3", Text: "later", Reply: true},
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "earlier", Reply: true},
		},
		Context: []modelv1.MsgLine{
			{ID: "C2", Ts: ts(2026, 3, 16, 9, 5, 0), Sender: "Dave", SenderID: "U4", Text: "after"},
			{ID: "C1", Ts: ts(2026, 3, 16, 8, 55, 0), Sender: "Eve", SenderID: "U5", Text: "before"},
		},
	}
	got := compact.CompactThread(f)
	if got.Replies[0].ID != "R1" || got.Replies[1].ID != "R2" {
		t.Errorf("replies not sorted: %v, %v", got.Replies[0].ID, got.Replies[1].ID)
	}
	if got.Context[0].ID != "C1" || got.Context[1].ID != "C2" {
		t.Errorf("context not sorted: %v, %v", got.Context[0].ID, got.Context[1].ID)
	}
}

func TestCompactThread_DedupMessages(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "first", Reply: true},
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "duplicate", Reply: true},
		},
	}
	got := compact.CompactThread(f)
	if len(got.Replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(got.Replies))
	}
	if got.Replies[0].Text != "first" {
		t.Errorf("reply text = %q, want %q", got.Replies[0].Text, "first")
	}
}

func TestCompactThread_DeleteRemovesReactionsForDeletedMessage(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "parent"},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "R1", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "R1", Sender: "Bob", SenderID: "U2"},
		},
	}
	got := compact.CompactThread(f)
	if len(got.Replies) != 0 {
		t.Fatalf("replies count = %d, want 0", len(got.Replies))
	}
	if len(got.Reactions) != 0 {
		t.Fatalf("reactions count = %d, want 0 (reaction for deleted reply should be removed)", len(got.Reactions))
	}
}

// --- AggregateReactions tests ---

func TestAggregateReactions_BasicGrouping(t *testing.T) {
	reactions := []modelv1.ReactLine{
		{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M2", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
		{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Charlie", SenderID: "U3", Emoji: "thumbsup"},
	}
	got := compact.AggregateReactions(reactions)
	if len(got["M1"]) != 2 {
		t.Fatalf("M1 reactions count = %d, want 2", len(got["M1"]))
	}
	if len(got["M2"]) != 1 {
		t.Fatalf("M2 reactions count = %d, want 1", len(got["M2"]))
	}
}

func TestAggregateReactions_ReactUnreactCancellation(t *testing.T) {
	reactions := []modelv1.ReactLine{
		{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
	}
	got := compact.AggregateReactions(reactions)
	if len(got["M1"]) != 0 {
		t.Fatalf("M1 reactions count = %d, want 0", len(got["M1"]))
	}
}

func TestAggregateReactions_MultipleEmojisPerMessage(t *testing.T) {
	reactions := []modelv1.ReactLine{
		{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "heart"},
		{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Emoji: "thumbsup"},
	}
	got := compact.AggregateReactions(reactions)
	if len(got["M1"]) != 3 {
		t.Fatalf("M1 reactions count = %d, want 3", len(got["M1"]))
	}
}

func TestAggregateReactions_Empty(t *testing.T) {
	got := compact.AggregateReactions(nil)
	if len(got) != 0 {
		t.Fatalf("result length = %d, want 0", len(got))
	}

	got = compact.AggregateReactions([]modelv1.ReactLine{})
	if len(got) != 0 {
		t.Fatalf("result length = %d, want 0", len(got))
	}
}

func TestAggregateReactions_ReactUnreactReact_Survives(t *testing.T) {
	reactions := []modelv1.ReactLine{
		{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup", Remove: true},
		{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
	}
	got := compact.AggregateReactions(reactions)
	if len(got["M1"]) != 1 {
		t.Fatalf("M1 reactions count = %d, want 1", len(got["M1"]))
	}
	if got["M1"][0].Remove {
		t.Error("surviving reaction should not be Remove=true")
	}
	if !got["M1"][0].Ts.Equal(ts(2026, 3, 16, 9, 3, 0)) {
		t.Errorf("surviving reaction ts = %v, want 09:03", got["M1"][0].Ts)
	}
}

// --- Edge case: edit then delete ---

func TestCompact_EditThenDelete(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "original"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 3, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1", Text: "edited"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Alice", SenderID: "U1"},
		},
	}
	got := compact.Compact(f)
	if len(got.Messages) != 0 {
		t.Fatalf("messages count = %d, want 0 (edited then deleted)", len(got.Messages))
	}
}

// --- Verify reactions are sorted after reconciliation ---

func TestCompact_ReactionsSortedAfterReconciliation(t *testing.T) {
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hi"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "M1", Sender: "Charlie", SenderID: "U3", Emoji: "wave"},
			{Ts: ts(2026, 3, 16, 9, 1, 0), MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
		},
	}
	got := compact.Compact(f)
	if len(got.Reactions) != 2 {
		t.Fatalf("reactions count = %d, want 2", len(got.Reactions))
	}
	if got.Reactions[0].Ts.After(got.Reactions[1].Ts) {
		t.Error("reactions not sorted by timestamp")
	}
}

// --- CompactThread integration test ---

func TestCompactThread_FullIntegration(t *testing.T) {
	f := &modelv1.ThreadFile{
		Parent: modelv1.MsgLine{
			ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0),
			Sender: "Alice", SenderID: "U1", Text: "thread start",
		},
		Replies: []modelv1.MsgLine{
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "original reply", Reply: true},
			{ID: "R2", Ts: ts(2026, 3, 16, 9, 3, 0), Sender: "Charlie", SenderID: "U3", Text: "will be deleted", Reply: true},
			{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "dup reply", Reply: true},
		},
		Context: []modelv1.MsgLine{
			{ID: "C1", Ts: ts(2026, 3, 16, 8, 55, 0), Sender: "Dave", SenderID: "U4", Text: "before"},
			{ID: "C2", Ts: ts(2026, 3, 16, 9, 10, 0), Sender: "Eve", SenderID: "U5", Text: "after"},
		},
		Reactions: []modelv1.ReactLine{
			{Ts: ts(2026, 3, 16, 9, 2, 0), MsgID: "P1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{Ts: ts(2026, 3, 16, 9, 4, 0), MsgID: "R2", Sender: "Alice", SenderID: "U1", Emoji: "heart"},
		},
		Edits: []modelv1.EditLine{
			{Ts: ts(2026, 3, 16, 9, 5, 0), MsgID: "R1", Sender: "Bob", SenderID: "U2", Text: "edited reply"},
		},
		Deletes: []modelv1.DeleteLine{
			{Ts: ts(2026, 3, 16, 9, 6, 0), MsgID: "R2", Sender: "Charlie", SenderID: "U3"},
		},
	}

	got := compact.CompactThread(f)

	// Parent survives, unmodified.
	if got.Parent.Text != "thread start" {
		t.Errorf("parent text = %q, want %q", got.Parent.Text, "thread start")
	}

	// R1 survives (deduped, edited), R2 deleted.
	if len(got.Replies) != 1 {
		t.Fatalf("replies count = %d, want 1", len(got.Replies))
	}
	if got.Replies[0].Text != "edited reply" {
		t.Errorf("reply text = %q, want %q", got.Replies[0].Text, "edited reply")
	}

	// Context unchanged.
	if len(got.Context) != 2 {
		t.Fatalf("context count = %d, want 2", len(got.Context))
	}

	// Reactions: thumbsup on P1 survives, heart on R2 deleted with R2.
	if len(got.Reactions) != 1 {
		t.Fatalf("reactions count = %d, want 1", len(got.Reactions))
	}
	if got.Reactions[0].MsgID != "P1" {
		t.Errorf("surviving reaction targets %q, want %q", got.Reactions[0].MsgID, "P1")
	}
}

// --- Verify timestamp is ts helper from encoding_test.go ---

func TestCompact_TimestampHandling(t *testing.T) {
	// Ensure we handle different timezones correctly (all comparisons in UTC).
	est := time.FixedZone("EST", -5*60*60)
	f := &modelv1.DateFile{
		Messages: []modelv1.MsgLine{
			{ID: "M2", Ts: time.Date(2026, 3, 16, 4, 15, 0, 0, est), Sender: "Bob", SenderID: "U2", Text: "second"}, // 09:15 UTC
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "first"},                // 09:00 UTC
		},
	}
	got := compact.Compact(f)
	if got.Messages[0].ID != "M1" {
		t.Errorf("first message should be M1 (09:00 UTC), got %q", got.Messages[0].ID)
	}
	if got.Messages[1].ID != "M2" {
		t.Errorf("second message should be M2 (09:15 UTC), got %q", got.Messages[1].ID)
	}
}

// --- DedupGWS tests ---

func gwsEmailLine(id, subject string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineEmail,
		Email: &modelv1.EmailLine{
			ID:      id,
			Subject: subject,
			Ts:      ts(2026, 4, 7, 12, 0, 0),
			From:    "test@example.com",
			To:      []string{"to@example.com"},
			Labels:  []string{"INBOX"},
		},
	}
}

func TestDedupGWS_KeepsLast(t *testing.T) {
	lines := []modelv1.Line{
		gwsEmailLine("same", "version-0"),
		gwsEmailLine("same", "version-1"),
		gwsEmailLine("same", "version-2"),
	}

	deduped := compact.DedupGWS(lines)
	if len(deduped) != 1 {
		t.Fatalf("got %d deduped lines, want 1", len(deduped))
	}
	if deduped[0].Email.Subject != "version-2" {
		t.Errorf("kept subject %q, want %q", deduped[0].Email.Subject, "version-2")
	}
}
