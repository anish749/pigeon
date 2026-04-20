package commands

import (
	"os"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

// createConvDirs creates empty conversation directories under the store root.
func createConvDirs(t *testing.T, root paths.DataRoot, acct account.Account, names ...string) {
	t.Helper()
	for _, name := range names {
		dir := root.AccountFor(acct).Conversation(name).Path()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create conv dir %s: %v", name, err)
		}
	}
}

func TestFindConversations_SubstringMatchesBoth(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "@Alice Smith", "#general")

	// "@alice" is a substring of both "@alice" and "@Alice Smith"
	matches, err := findConversations(s, acct, "@alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_MultipleSubstring(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "@Alice Smith", "@mpdm-alice--bob-1")

	matches, err := findConversations(s, acct, "alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_CaseInsensitive(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@Alice", "#general")

	matches, err := findConversations(s, acct, "alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
}

func TestFindConversations_NoMatch(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice", "#general")

	_, err := findConversations(s, acct, "bob", nil)
	if err == nil {
		t.Fatal("expected error for no match")
	}
}

func TestFindConversations_MatchesAlias(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "+14155551234_Alice", "#general")

	aliases := map[string][]string{
		"+14155551234_Alice": {"Mom"},
	}

	matches, err := findConversations(s, acct, "mom", aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
	if matches[0].displayName != "Mom" {
		t.Errorf("expected display name Mom, got %s", matches[0].displayName)
	}
}

func TestFindConversations_DisplayNameMatch(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "+14155551234_Alice", "+14155559876_Bob")

	matches, err := findConversations(s, acct, "Alice", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), convNames(matches))
	}
	if matches[0].dirName != "+14155551234_Alice" {
		t.Errorf("expected +14155551234_Alice, got %s", matches[0].dirName)
	}
}

func TestFindConversations_NoDuplicateOnAliasAndDirName(t *testing.T) {
	s, root := setupStore(t)
	acct := account.New("slack", "test-ws")
	createConvDirs(t, root, acct, "@alice")

	aliases := map[string][]string{
		"@alice": {"alice-wonderland"},
	}

	matches, err := findConversations(s, acct, "alice", aliases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match (no duplicate from alias), got %d: %v", len(matches), convNames(matches))
	}
}

func convNames(matches []*conversation) []string {
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.dirName
	}
	return names
}

func TestActiveConversations(t *testing.T) {
	root := "/data"
	files := []string{
		"/data/slack/acme/#general/2026-04-07.jsonl",
		"/data/slack/acme/#general/threads/1742100000.jsonl",
		"/data/slack/acme/#random/2026-04-07.jsonl",
		"/data/whatsapp/phone/Alice/2026-04-05.jsonl",
	}

	convs := activeConversations(files, root)

	if len(convs) != 3 {
		t.Fatalf("got %d conversations, want 3", len(convs))
	}
	if convs[0].Display != "slack/acme/#general" {
		t.Errorf("convs[0].Display = %q, want slack/acme/#general", convs[0].Display)
	}
	if convs[0].Dir != "/data/slack/acme/#general" {
		t.Errorf("convs[0].Dir = %q, want /data/slack/acme/#general", convs[0].Dir)
	}
	if convs[1].Display != "slack/acme/#random" {
		t.Errorf("convs[1].Display = %q, want slack/acme/#random", convs[1].Display)
	}
	if convs[2].Display != "whatsapp/phone/Alice" {
		t.Errorf("convs[2].Display = %q, want whatsapp/phone/Alice", convs[2].Display)
	}
}

func TestActiveConversations_ConversationNamedThreads(t *testing.T) {
	root := "/data"
	files := []string{
		"/data/slack/acme/threads/2026-04-07.jsonl",
		"/data/slack/acme/threads/threads/1742100000.jsonl",
	}

	convs := activeConversations(files, root)

	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	if convs[0].Display != "slack/acme/threads" {
		t.Errorf("Display = %q, want slack/acme/threads", convs[0].Display)
	}
}
