package slack

import (
	"testing"

	"github.com/anish749/pigeon/internal/identity"
)

func TestResolveMentions(t *testing.T) {
	writer := testIdentityWriter(t, "test-ws", []identity.Signal{
		{
			Name: "Alice Johnson",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U111",
				DisplayName: "alice",
				RealName:    "Alice Johnson",
				Name:        "alice.johnson",
			},
		},
		{
			Name: "Bob Smith",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U222",
				DisplayName: "bob",
				RealName:    "Bob Smith",
				Name:        "bob",
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
		name string
		text string
		want string
	}{
		{
			name: "single mention",
			text: "hey @alice can you check this?",
			want: "hey <@U111> can you check this?",
		},
		{
			name: "mention at start",
			text: "@bob please review",
			want: "<@U222> please review",
		},
		{
			name: "multiple mentions",
			text: "@alice and @bob please sync up",
			want: "<@U111> and <@U222> please sync up",
		},
		{
			name: "unknown mention left as-is",
			text: "hey @unknown-person check this",
			want: "hey @unknown-person check this",
		},
		{
			name: "channel broadcast",
			text: "@channel deploy is done",
			want: "<!channel> deploy is done",
		},
		{
			name: "here broadcast",
			text: "@here standup time",
			want: "<!here> standup time",
		},
		{
			name: "everyone broadcast",
			text: "@everyone important update",
			want: "<!everyone> important update",
		},
		{
			name: "no mentions",
			text: "just a regular message",
			want: "just a regular message",
		},
		{
			name: "mention by username",
			text: "cc @bob on this",
			want: "cc <@U222> on this",
		},
		{
			name: "email not treated as mention",
			text: "send to user@example.com",
			want: "send to user@example.com",
		},
		{
			name: "mixed broadcast and user",
			text: "@here @alice is on call today",
			want: "<!here> <@U111> is on call today",
		},
		{
			name: "pre-resolved mentions left as-is",
			text: "<!here> <@U111> already formatted",
			want: "<!here> <@U111> already formatted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ResolveMentions(tt.text)
			if got != tt.want {
				t.Errorf("ResolveMentions(%q)\n  got:  %q\n  want: %q", tt.text, got, tt.want)
			}
		})
	}
}
