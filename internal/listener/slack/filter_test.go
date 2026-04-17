package slack

import (
	"context"
	"testing"

	goslack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

func TestShouldKeepMessage(t *testing.T) {
	tests := []struct {
		name    string
		subType string
		text    string
		want    bool
	}{
		// Kept: conversational content with text.
		{"regular message", "", "hello", true},
		{"bot message", "bot_message", "k8s alert", true},
		{"thread broadcast", "thread_broadcast", "also posted to channel", true},
		{"assistant app thread", "assistant_app_thread", "AI response", true},

		// Skipped: system/structural events.
		{"channel join", "channel_join", "joined", false},
		{"channel leave", "channel_leave", "left", false},
		{"channel topic", "channel_topic", "set topic", false},
		{"channel purpose", "channel_purpose", "set purpose", false},
		{"file share", "file_share", "uploaded a file", false},
		{"me message", "me_message", "is typing", false},
		{"pinned item", "pinned_item", "pinned a message", false},
		{"unpinned item", "unpinned_item", "unpinned a message", false},
		{"huddle thread", "huddle_thread", "huddle started", false},

		// Skipped: empty text regardless of subtype.
		{"empty text regular", "", "", false},
		{"empty text bot", "bot_message", "", false},
		{"empty text broadcast", "thread_broadcast", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldKeepMessage(tt.subType, tt.text)
			if got != tt.want {
				t.Errorf("shouldKeepMessage(%q, %q) = %v, want %v", tt.subType, tt.text, got, tt.want)
			}
		})
	}
}

func TestShouldAutoReply(t *testing.T) {
	tests := []struct {
		name       string
		pigeonBotUID  string
		msg        *slackevents.MessageEvent
		routeState hub.RouteState
		isBotDM    bool
		want       bool
	}{
		{"other user DMs bot", "U_BOT",
			&slackevents.MessageEvent{User: "U_OTHER"}, hub.RouteNoSession, true, true},
		{"bot's own message by user ID", "U_BOT",
			&slackevents.MessageEvent{User: "U_BOT"}, hub.RouteNoSession, true, false},
		{"bot's own message by bot ID", "U_BOT",
			&slackevents.MessageEvent{BotID: "U_BOT"}, hub.RouteNoSession, true, false},
		{"not a bot DM", "U_BOT",
			&slackevents.MessageEvent{User: "U_OTHER"}, hub.RouteNoSession, false, false},
		{"session exists", "U_BOT",
			&slackevents.MessageEvent{User: "U_OTHER"}, hub.RouteOK, true, false},
		{"empty pigeonBotUID still allows reply", "",
			&slackevents.MessageEvent{User: "U_OTHER"}, hub.RouteNoSession, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoReply(tt.pigeonBotUID, tt.msg, tt.routeState, tt.isBotDM)
			if got != tt.want {
				t.Errorf("shouldAutoReply(%q, msg, %v, %v) = %v, want %v",
					tt.pigeonBotUID, tt.routeState, tt.isBotDM, got, tt.want)
			}
		})
	}
}

func testIdentityWriter(t *testing.T, workspace string, signals []identity.Signal) *identity.Writer {
	t.Helper()
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	w := identity.NewWriter(s, root.AccountFor(account.New("slack", workspace)).Identity())
	if err := w.ObserveBatch(signals); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestSenderName(t *testing.T) {
	writer := testIdentityWriter(t, "test-ws", []identity.Signal{
		{
			Name: "alice",
			Slack: &identity.SlackIdentity{
				Workspace: "test-ws", ID: "U123", DisplayName: "alice",
			},
		},
		{
			Name: "PagerDuty",
			Slack: &identity.SlackIdentity{
				Workspace: "test-ws", ID: "B789", DisplayName: "PagerDuty",
			},
		},
	})
	r := &Resolver{
		writer:    writer,
		workspace: "test-ws",
		channels:  make(map[string]string),
		dmUsers:   make(map[string]string),
		members:   make(map[string]bool),
	}

	tests := []struct {
		name     string
		userID   string
		botID    string
		username string
		wantName string
		wantID   string
		wantErr  bool
	}{
		{
			name:     "human user",
			userID:   "U123",
			wantName: "alice",
			wantID:   "U123",
		},
		{
			name:     "bot with username",
			botID:    "B456",
			username: "GitHub",
			wantName: "GitHub",
			wantID:   "B456",
		},
		{
			name:     "bot with name in identity",
			botID:    "B789",
			wantName: "PagerDuty",
			wantID:   "B789",
		},
		{
			name:    "no identifiers",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, id, err := r.SenderName(context.Background(), goslack.Msg{User: tt.userID, BotID: tt.botID, Username: tt.username})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got name=%q id=%q", name, id)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}
