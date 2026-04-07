package cli

import (
	"testing"
)

func TestExtractConversations(t *testing.T) {
	root := "/data"
	files := []string{
		"/data/slack/acme/#general/2026-04-07.jsonl",
		"/data/slack/acme/#general/2026-04-06.jsonl",
		"/data/slack/acme/#general/threads/1742100000.jsonl",
		"/data/slack/acme/#random/2026-04-07.jsonl",
		"/data/whatsapp/phone/Alice/2026-04-05.jsonl",
	}

	convs := extractConversations(files, root)

	if len(convs) != 3 {
		t.Fatalf("got %d conversations, want 3", len(convs))
	}

	// #general: has both date files and threads — should pick latest date.
	if convs[0].Display != "slack/acme/#general" {
		t.Errorf("convs[0].Display = %q, want slack/acme/#general", convs[0].Display)
	}
	if convs[0].LatestDate != "2026-04-07" {
		t.Errorf("convs[0].LatestDate = %q, want 2026-04-07", convs[0].LatestDate)
	}
	if convs[0].Dir != "/data/slack/acme/#general" {
		t.Errorf("convs[0].Dir = %q, want /data/slack/acme/#general", convs[0].Dir)
	}

	// #random: single date file.
	if convs[1].Display != "slack/acme/#random" {
		t.Errorf("convs[1].Display = %q, want slack/acme/#random", convs[1].Display)
	}

	// Alice: whatsapp conversation.
	if convs[2].Display != "whatsapp/phone/Alice" {
		t.Errorf("convs[2].Display = %q, want whatsapp/phone/Alice", convs[2].Display)
	}
}

func TestExtractConversations_ThreadOnly(t *testing.T) {
	root := "/data"
	files := []string{
		"/data/slack/acme/#general/threads/1742100000.jsonl",
		"/data/slack/acme/#general/threads/1742200000.jsonl",
	}

	convs := extractConversations(files, root)

	if len(convs) != 1 {
		t.Fatalf("got %d conversations, want 1", len(convs))
	}
	if convs[0].LatestDate != "" {
		t.Errorf("thread-only conversation should have empty LatestDate, got %q", convs[0].LatestDate)
	}
}

func TestExtractConversations_Empty(t *testing.T) {
	convs := extractConversations(nil, "/data")
	if len(convs) != 0 {
		t.Errorf("got %d conversations, want 0", len(convs))
	}
}

func TestExtractConversations_PreservesOrder(t *testing.T) {
	root := "/data"
	files := []string{
		"/data/slack/acme/#beta/2026-04-07.jsonl",
		"/data/slack/acme/#alpha/2026-04-07.jsonl",
	}

	convs := extractConversations(files, root)

	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convs))
	}
	// Order should match first-seen in the input.
	if convs[0].Display != "slack/acme/#beta" {
		t.Errorf("first conversation = %q, want slack/acme/#beta", convs[0].Display)
	}
}
