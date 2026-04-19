package api

import "testing"

func TestResolveSlackMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text unchanged",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "markdown bold to mrkdwn",
			in:   "this is **bold**",
			want: "this is *bold*",
		},
		{
			name: "shell escape reversed",
			in:   `nice patch\! looks good`,
			want: "nice patch! looks good",
		},
		{
			name: "both transformations",
			in:   `**great work**\! ship it`,
			want: "*great work*! ship it",
		},
		{
			name: "no double unescape",
			in:   "already has !",
			want: "already has !",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveSlackMessage(tt.in); got != tt.want {
				t.Fatalf("ResolveSlackMessage(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
