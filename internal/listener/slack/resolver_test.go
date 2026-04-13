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
		{
			Name: "Sherlock Holmes",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U333",
				DisplayName: "Sherlock Holmes",
				RealName:    "Sherlock Holmes",
				Name:        "sherlock.holmes",
			},
		},
		{
			Name: "Björk Guðmundsdóttir",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U555",
				DisplayName: "Björk",
				RealName:    "Björk Guðmundsdóttir",
				Name:        "bjork",
			},
		},
		{
			Name: "Björn Borg",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U666",
				DisplayName: "Björn Borg",
				RealName:    "Björn Borg",
				Name:        "bjorn.borg",
			},
		},
		{
			Name: "Sherlock Watson",
			Slack: &identity.SlackIdentity{
				Workspace:   "test-ws",
				ID:          "U444",
				DisplayName: "Sherlock Watson",
				RealName:    "Sherlock Watson",
				Name:        "sherlock.watson",
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
		{
			name: "multi-word name consumes full name",
			text: "don't worry @Sherlock Holmes we've got this",
			want: "don't worry <@U333> we've got this",
		},
		{
			name: "multi-word name at end of text",
			text: "thanks @Sherlock Holmes",
			want: "thanks <@U333>",
		},
		{
			name: "multi-word name at start",
			text: "@Sherlock Holmes please review",
			want: "<@U333> please review",
		},
		{
			name: "multi-word ambiguous first name leaves as-is",
			text: "hey @Sherlock what do you think?",
			want: "hey @Sherlock what do you think?",
		},
		{
			name: "two multi-word mentions",
			text: "@Sherlock Holmes and @Alice Johnson sync up",
			want: "<@U333> and <@U111> sync up",
		},
		{
			name: "multi-word name case insensitive",
			text: "hey @sherlock holmes check this",
			want: "hey <@U333> check this",
		},
		{
			name: "unicode name matched by ascii username",
			text: "hey @bjork great show last night",
			want: "hey <@U555> great show last night",
		},
		{
			name: "unicode word boundary prevents partial match",
			text: "hey @bjorkést check this",
			want: "hey @bjorkést check this",
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
