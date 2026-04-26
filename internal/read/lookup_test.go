package read_test

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestLookupMessage_DateFile(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	msg := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
			Sender: "Alice", SenderID: "U001", Text: "hello world",
		},
	}
	if err := s.Append(acct, "#general", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := read.LookupMessage(root.AccountFor(acct).Conversation("#general"), "1700000001.000001")
	if got == nil {
		t.Fatal("LookupMessage returned nil, want match")
	}
	if got.Sender != "Alice" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Alice")
	}
	if got.Text != "hello world" {
		t.Errorf("Text = %q, want %q", got.Text, "hello world")
	}
}

func TestLookupMessage_ThreadFile(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	reply := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000002.000002", Ts: time.Date(2026, 4, 19, 10, 5, 0, 0, time.UTC),
			Sender: "Bob", SenderID: "U002", Text: "thread reply", Reply: true,
		},
	}
	if err := s.AppendThread(acct, "#general", "1700000001.000001", reply); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}

	got := read.LookupMessage(root.AccountFor(acct).Conversation("#general"), "1700000002.000002")
	if got == nil {
		t.Fatal("LookupMessage returned nil, want match")
	}
	if got.Sender != "Bob" {
		t.Errorf("Sender = %q, want %q", got.Sender, "Bob")
	}
	if got.Text != "thread reply" {
		t.Errorf("Text = %q, want %q", got.Text, "thread reply")
	}
}

func TestLookupMessage_NotFound(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	acct := account.New("slack", "acme-corp")

	msg := modelv1.Line{
		Type: modelv1.LineMessage,
		Msg: &modelv1.MsgLine{
			ID: "1700000001.000001", Ts: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
			Sender: "Alice", SenderID: "U001", Text: "hello",
		},
	}
	if err := s.Append(acct, "#general", msg); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := read.LookupMessage(root.AccountFor(acct).Conversation("#general"), "9999999999.999999")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLookupMessage_NoConversation(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	acct := account.New("slack", "acme-corp")

	got := read.LookupMessage(root.AccountFor(acct).Conversation("#nonexistent"), "1700000001.000001")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
