package ingress

import (
	"context"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestStoreSourceListSignals_ConversationMessagesAfterCursor(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	fs := store.NewFSStore(root)
	source := NewStoreSource(root)
	acct := account.New("slack", "acme")

	before := time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC)
	after := before.Add(15 * time.Minute)

	if err := fs.Append(acct, "general", modelv1.Line{
		Type: modelv1.LineMessage,
		Msg:  &modelv1.MsgLine{ID: "m-1", Ts: before, Sender: "alice", Text: "before"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Append(acct, "general", modelv1.Line{
		Type: modelv1.LineMessage,
		Msg:  &modelv1.MsgLine{ID: "m-2", Ts: after, Sender: "bob", Text: "after"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := source.ListSignals(context.Background(), acct, "general", before)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(got))
	}
	if got[0].ID != "m-2" {
		t.Fatalf("expected m-2, got %s", got[0].ID)
	}
	if got[0].ThreadID != "" {
		t.Fatalf("expected empty thread id, got %q", got[0].ThreadID)
	}
}

func TestStoreSourceListSignals_IncludesThreadReplies(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	fs := store.NewFSStore(root)
	source := NewStoreSource(root)
	acct := account.New("slack", "acme")

	parentTs := time.Date(2026, 4, 19, 8, 0, 0, 0, time.UTC)
	replyTs := parentTs.Add(10 * time.Minute)

	if err := fs.Append(acct, "general", modelv1.Line{
		Type: modelv1.LineMessage,
		Msg:  &modelv1.MsgLine{ID: "parent", Ts: parentTs, Sender: "alice", Text: "root"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := fs.AppendThread(acct, "general", "1713513600.000100", modelv1.Line{
		Type: modelv1.LineMessage,
		Msg:  &modelv1.MsgLine{ID: "reply", Ts: replyTs, Sender: "bob", Text: "thread reply", Reply: true},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := source.ListSignals(context.Background(), acct, "general", parentTs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(got))
	}
	if got[0].ID != "reply" {
		t.Fatalf("expected reply signal, got %s", got[0].ID)
	}
	if got[0].ThreadID != "1713513600.000100" {
		t.Fatalf("expected thread id 1713513600.000100, got %q", got[0].ThreadID)
	}
}

func TestStoreSourceListSignals_UnsupportedPlatform(t *testing.T) {
	source := NewStoreSource(paths.NewDataRoot(t.TempDir()))

	_, err := source.ListSignals(context.Background(), account.New("gws", "acme"), "general", time.Time{})
	if err == nil {
		t.Fatal("expected unsupported platform error")
	}
}
