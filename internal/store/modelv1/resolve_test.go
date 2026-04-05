package modelv1

import (
	"testing"
)

func TestResolve_GroupsReactions(t *testing.T) {
	f := &DateFile{
		Messages: []MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
			{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "world"},
		},
		Reactions: []ReactLine{
			{MsgID: "M1", Sender: "Bob", SenderID: "U2", Emoji: "thumbsup"},
			{MsgID: "M1", Sender: "Charlie", SenderID: "U3", Emoji: "heart"},
			{MsgID: "M2", Sender: "Alice", SenderID: "U1", Emoji: "tada"},
		},
	}

	resolved := Resolve(f)

	if len(resolved.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(resolved.Messages))
	}
	if len(resolved.Messages[0].Reactions) != 2 {
		t.Errorf("M1 reactions = %d, want 2", len(resolved.Messages[0].Reactions))
	}
	if len(resolved.Messages[1].Reactions) != 1 {
		t.Errorf("M2 reactions = %d, want 1", len(resolved.Messages[1].Reactions))
	}
}

func TestResolve_NoReactions(t *testing.T) {
	f := &DateFile{
		Messages: []MsgLine{
			{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "hello"},
		},
	}

	resolved := Resolve(f)

	if len(resolved.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(resolved.Messages))
	}
	if resolved.Messages[0].Reactions != nil {
		t.Errorf("expected nil reactions, got %v", resolved.Messages[0].Reactions)
	}
}

func TestResolve_Empty(t *testing.T) {
	resolved := Resolve(&DateFile{})
	if len(resolved.Messages) != 0 {
		t.Errorf("messages = %d, want 0", len(resolved.Messages))
	}
}

func TestResolve_Nil(t *testing.T) {
	resolved := Resolve(nil)
	if resolved == nil {
		t.Fatal("expected non-nil ResolvedDateFile")
	}
	if len(resolved.Messages) != 0 {
		t.Errorf("messages = %d, want 0", len(resolved.Messages))
	}
}

func TestResolveThread(t *testing.T) {
	f := &ThreadFile{
		Parent:  MsgLine{ID: "P1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", SenderID: "U1", Text: "thread start"},
		Replies: []MsgLine{{ID: "R1", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", SenderID: "U2", Text: "reply", Reply: true}},
		Context: []MsgLine{{ID: "C1", Ts: ts(2026, 3, 16, 9, 2, 0), Sender: "Charlie", SenderID: "U3", Text: "context"}},
		Reactions: []ReactLine{
			{MsgID: "P1", Sender: "Bob", SenderID: "U2", Emoji: "tada"},
			{MsgID: "R1", Sender: "Alice", SenderID: "U1", Emoji: "thumbsup"},
		},
	}

	resolved := ResolveThread(f)

	if resolved == nil {
		t.Fatal("expected non-nil")
	}
	if len(resolved.Parent.Reactions) != 1 {
		t.Errorf("parent reactions = %d, want 1", len(resolved.Parent.Reactions))
	}
	if len(resolved.Replies) != 1 || len(resolved.Replies[0].Reactions) != 1 {
		t.Errorf("reply reactions = %d, want 1", len(resolved.Replies[0].Reactions))
	}
	if len(resolved.Context) != 1 || resolved.Context[0].Reactions != nil {
		t.Errorf("context should have no reactions")
	}
}

func TestResolveThread_Nil(t *testing.T) {
	if ResolveThread(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestResolve_PreservesMessageFields(t *testing.T) {
	f := &DateFile{
		Messages: []MsgLine{
			{
				ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0),
				Sender: "Alice", SenderID: "U1",
				Via: ViaPigeonAsUser, ReplyTo: "Q1",
				Text: "hello", Reply: true,
				Attachments: []Attachment{{ID: "A1", Type: "image/jpeg"}},
			},
		},
	}

	resolved := Resolve(f)
	m := resolved.Messages[0]

	if m.ID != "M1" || m.Sender != "Alice" || m.SenderID != "U1" {
		t.Error("basic fields not preserved")
	}
	if m.Via != ViaPigeonAsUser || m.ReplyTo != "Q1" || !m.Reply {
		t.Error("via/replyTo/reply not preserved")
	}
	if len(m.Attachments) != 1 || m.Attachments[0].ID != "A1" {
		t.Error("attachments not preserved")
	}
}
