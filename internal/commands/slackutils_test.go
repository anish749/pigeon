package commands

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func setupStore(t *testing.T) (*store.FSStore, paths.DataRoot) {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	return store.NewFSStore(root), root
}

func msg(id string, ts time.Time, sender, senderID, text string) modelv1.Line {
	return modelv1.Line{
		Type: modelv1.LineMessage,
		Msg:  &modelv1.MsgLine{ID: id, Ts: ts, Sender: sender, SenderID: senderID, Text: text},
	}
}

func TestMessageExists_Found(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "acme-corp")

	if err := s.Append(acct, "#general", msg("1711568940.789012", time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC), "Alice", "U1", "hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	accountDir := root.AccountFor(acct).Path()
	found, err := messageExists(accountDir, "1711568940.789012")
	if err != nil {
		t.Fatalf("messageExists: %v", err)
	}
	if !found {
		t.Error("expected message to be found")
	}
}

func TestMessageExists_NotFound(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "acme-corp")

	if err := s.Append(acct, "#general", msg("1711568940.789012", time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC), "Alice", "U1", "hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	accountDir := root.AccountFor(acct).Path()
	found, err := messageExists(accountDir, "9999999999.000000")
	if err != nil {
		t.Fatalf("messageExists: %v", err)
	}
	if found {
		t.Error("expected nonexistent message not to be found")
	}
}

// TestMessageExists_ThreadParentWithoutReplies verifies that a message used as
// a thread parent can be found even before it has any thread replies (i.e. no
// thread file exists yet).
func TestMessageExists_ThreadParentWithoutReplies(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "acme-corp")

	if err := s.Append(acct, "#engineering", msg("1711568940.789012", time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC), "Bob", "U2", "start a thread")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	accountDir := root.AccountFor(acct).Path()

	// No thread file exists yet.
	if s.ThreadExists(acct, "#engineering", "1711568940.789012") {
		t.Error("ThreadExists should be false — no replies yet")
	}
	// But the message itself is findable.
	found, err := messageExists(accountDir, "1711568940.789012")
	if err != nil {
		t.Fatalf("messageExists: %v", err)
	}
	if !found {
		t.Error("expected thread parent message to be found even without replies")
	}
}

// TestMessageExists_AcrossConversations verifies that messageExists searches
// the whole account — channels, DMs, and MPDMs alike.
func TestMessageExists_AcrossConversations(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "acme-corp")

	// Write to a DM-style conversation.
	if err := s.Append(acct, "@alice", msg("1711568941.000001", time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC), "Alice", "U1", "hey")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	accountDir := root.AccountFor(acct).Path()

	// Message from a different conversation is still found.
	found, err := messageExists(accountDir, "1711568941.000001")
	if err != nil {
		t.Fatalf("messageExists: %v", err)
	}
	if !found {
		t.Error("expected message from DM conversation to be found via account-wide search")
	}
}
