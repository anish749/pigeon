package slack

import (
	"context"
	"testing"
)

func TestAllowedSubType(t *testing.T) {
	allowed := []string{"", "thread_broadcast", "bot_message"}
	for _, st := range allowed {
		if !allowedSubType(st) {
			t.Errorf("allowedSubType(%q) = false, want true", st)
		}
	}

	blocked := []string{
		"channel_join", "channel_leave", "channel_topic",
		"channel_purpose", "file_share", "me_message",
	}
	for _, st := range blocked {
		if allowedSubType(st) {
			t.Errorf("allowedSubType(%q) = true, want false", st)
		}
	}
}

// stubResolver implements just enough of Resolver for resolveSender tests.
type stubResolver struct {
	users map[string]string
}

func (r *stubResolver) UserName(_ context.Context, userID string) string {
	if name, ok := r.users[userID]; ok {
		return name
	}
	return userID
}

func TestResolveSender(t *testing.T) {
	// Build a minimal Resolver with a pre-populated cache.
	r := &Resolver{users: map[string]string{"U123": "alice", "B789": "PagerDuty"}}

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
			name:     "bot with name in cache",
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
			name, id, err := resolveSender(context.Background(), r, tt.userID, tt.botID, tt.username)
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
