package hub

import (
	"sort"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
)

func TestConnectedClaudeSessions_Empty(t *testing.T) {
	h := &Hub{
		sessions: make(map[string]*Session),
		channels: make(map[string]*channel),
	}
	got := h.ConnectedClaudeSessions()
	if len(got) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(got))
	}
}

func TestConnectedClaudeSessions_SingleSession(t *testing.T) {
	h := &Hub{
		sessions: map[string]*Session{
			"sess-1": {SessionID: "sess-1", CWD: "/home/user/project"},
		},
		channels: map[string]*channel{
			"slack-acme": {
				acct:      account.New("slack", "Acme Corp"),
				sessionID: "sess-1",
			},
		},
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
	if got[0].SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", got[0].SessionID, "sess-1")
	}
	if got[0].CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want %q", got[0].CWD, "/home/user/project")
	}
	if got[0].Account != "slack/Acme Corp" {
		t.Errorf("Account = %q, want %q", got[0].Account, "slack/Acme Corp")
	}
}

func TestConnectedClaudeSessions_MultipleSessions(t *testing.T) {
	h := &Hub{
		sessions: map[string]*Session{
			"sess-1": {SessionID: "sess-1", CWD: "/home/user/project-a"},
			"sess-2": {SessionID: "sess-2", CWD: "/home/user/project-b"},
		},
		channels: map[string]*channel{
			"slack-acme": {
				acct:      account.New("slack", "Acme Corp"),
				sessionID: "sess-1",
			},
			"whatsapp-phone": {
				acct:      account.New("whatsapp", "+1234567890"),
				sessionID: "sess-2",
			},
		},
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}

	// Sort for deterministic comparison since map iteration is unordered.
	sort.Slice(got, func(i, j int) bool { return got[i].SessionID < got[j].SessionID })

	if got[0].SessionID != "sess-1" || got[0].Account != "slack/Acme Corp" {
		t.Errorf("session[0] = %+v, want sess-1 / slack/Acme Corp", got[0])
	}
	if got[1].SessionID != "sess-2" || got[1].Account != "whatsapp/+1234567890" {
		t.Errorf("session[1] = %+v, want sess-2 / whatsapp/+1234567890", got[1])
	}
}

func TestConnectedClaudeSessions_SessionWithNoChannel(t *testing.T) {
	// A session exists but no channel points to it — Account should be empty.
	h := &Hub{
		sessions: map[string]*Session{
			"sess-orphan": {SessionID: "sess-orphan", CWD: "/tmp"},
		},
		channels: make(map[string]*channel),
	}

	got := h.ConnectedClaudeSessions()
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
	if got[0].Account != "" {
		t.Errorf("Account = %q, want empty for orphan session", got[0].Account)
	}
}

func TestParseSlackTimestamp(t *testing.T) {
	tests := []struct {
		name string
		ts   string
		want time.Time
	}{
		{
			name: "valid timestamp",
			ts:   "1712345678.123456",
			want: time.Unix(1712345678, 0),
		},
		{
			name: "no fractional part",
			ts:   "1712345678",
			want: time.Unix(1712345678, 0),
		},
		{
			name: "invalid",
			ts:   "not-a-timestamp",
			want: time.Time{},
		},
		{
			name: "empty",
			ts:   "",
			want: time.Time{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSlackTimestamp(tt.ts)
			if !got.Equal(tt.want) {
				t.Errorf("parseSlackTimestamp(%q) = %v, want %v", tt.ts, got, tt.want)
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{
			name:   "short text unchanged",
			s:      "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			s:      "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long text truncated",
			s:      "hello world",
			maxLen: 5,
			want:   "hello...",
		},
		{
			name:   "empty string",
			s:      "",
			maxLen: 10,
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateText(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}
