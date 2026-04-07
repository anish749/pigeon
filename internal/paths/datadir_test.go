package paths

import (
	"testing"

	"github.com/anish749/pigeon/internal/account"
)

func TestDataRoot_Platform(t *testing.T) {
	root := NewDataRoot("/tmp/test")
	got := root.Platform("slack").Path()
	if got != "/tmp/test/slack" {
		t.Errorf("Platform(slack).Path() = %q, want /tmp/test/slack", got)
	}
}

func TestDataRoot_Platform_LowercasesName(t *testing.T) {
	root := NewDataRoot("/tmp/test")
	got := root.Platform("Slack").Path()
	if got != "/tmp/test/slack" {
		t.Errorf("Platform(Slack).Path() = %q, want /tmp/test/slack", got)
	}
}

func TestAccountFor_Slugifies(t *testing.T) {
	root := NewDataRoot("/tmp/test")
	acct := account.New("slack", "Coding With Anish")
	got := root.AccountFor(acct).Path()
	if got != "/tmp/test/slack/coding-with-anish" {
		t.Errorf("AccountFor().Path() = %q, want /tmp/test/slack/coding-with-anish", got)
	}
}

func TestPlatformDir_AccountFromSlug(t *testing.T) {
	root := NewDataRoot("/tmp/test")
	got := root.Platform("slack").AccountFromSlug("coding-with-anish").Path()
	if got != "/tmp/test/slack/coding-with-anish" {
		t.Errorf("AccountFromSlug().Path() = %q, want /tmp/test/slack/coding-with-anish", got)
	}
}

func TestAccountDir_Conversation(t *testing.T) {
	acct := NewDataRoot("/tmp/test").AccountFor(account.New("slack", "My Workspace"))
	got := acct.Conversation("#general").Path()
	if got != "/tmp/test/slack/my-workspace/#general" {
		t.Errorf("Conversation().Path() = %q, want /tmp/test/slack/my-workspace/#general", got)
	}
}

func TestConversationDir_DateFile(t *testing.T) {
	conv := NewDataRoot("/tmp/test").Platform("slack").AccountFromSlug("ws").Conversation("#general")
	got := conv.DateFile("2024-01-15")
	if got != "/tmp/test/slack/ws/#general/2024-01-15.jsonl" {
		t.Errorf("DateFile() = %q, want /tmp/test/slack/ws/#general/2024-01-15.jsonl", got)
	}
}

func TestConversationDir_ThreadsDir(t *testing.T) {
	conv := NewDataRoot("/tmp/test").Platform("slack").AccountFromSlug("ws").Conversation("#general")
	got := conv.ThreadsDir()
	if got != "/tmp/test/slack/ws/#general/threads" {
		t.Errorf("ThreadsDir() = %q, want /tmp/test/slack/ws/#general/threads", got)
	}
}

func TestConversationDir_ThreadFile(t *testing.T) {
	conv := NewDataRoot("/tmp/test").Platform("slack").AccountFromSlug("ws").Conversation("#general")
	got := conv.ThreadFile("1234567890.123456")
	if got != "/tmp/test/slack/ws/#general/threads/1234567890.123456.jsonl" {
		t.Errorf("ThreadFile() = %q, want /tmp/test/slack/ws/#general/threads/1234567890.123456.jsonl", got)
	}
}

func TestAccountDir_SyncCursorsPath(t *testing.T) {
	acct := NewDataRoot("/tmp/test").Platform("slack").AccountFromSlug("ws")
	got := acct.SyncCursorsPath()
	if got != "/tmp/test/slack/ws/.sync-cursors.yaml" {
		t.Errorf("SyncCursorsPath() = %q, want /tmp/test/slack/ws/.sync-cursors.yaml", got)
	}
}

func TestAccountDir_MaintenancePath(t *testing.T) {
	acct := NewDataRoot("/tmp/test").Platform("slack").AccountFromSlug("ws")
	got := acct.MaintenancePath()
	if got != "/tmp/test/slack/ws/.maintenance.json" {
		t.Errorf("MaintenancePath() = %q, want /tmp/test/slack/ws/.maintenance.json", got)
	}
}

func TestIsThreadFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/data/slack/ws/#general/threads/1742100000.jsonl", true},
		{"/data/slack/ws/#general/2026-04-07.jsonl", false},
		{"/data/slack/ws/#general/threads/1742100000.123456.jsonl", true},
		{"threads/1742100000.jsonl", true},
		{"/data/slack/ws/#threads/2026-04-07.jsonl", false}, // conversation named #threads, not a thread file
		{"", false},
	}
	for _, tt := range tests {
		if got := IsThreadFile(tt.path); got != tt.want {
			t.Errorf("IsThreadFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDefaultDataRoot_UsesEnv(t *testing.T) {
	t.Setenv("PIGEON_DATA_DIR", "/tmp/pigeon-test")
	got := DefaultDataRoot().AccountFor(account.New("whatsapp", "+15551234567")).Path()
	if got != "/tmp/pigeon-test/whatsapp/15551234567" {
		t.Errorf("got %q, want /tmp/pigeon-test/whatsapp/15551234567", got)
	}
}
