package slack

import (
	"context"
	"testing"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestShouldRouteInChannel(t *testing.T) {
	const channel = "#general"
	const threadTS = "P1"

	tests := []struct {
		name     string
		text     string
		threadTS string
		// fixture: lines written to the thread file before the predicate runs.
		// Only used when the case wants to exercise the participation branch.
		threadLines []modelv1.Line
		want        bool
	}{
		{
			name: "no mention, top-level → false",
			text: "hello world",
			want: false,
		},
		{
			name: "explicit mention → true",
			text: "<@" + botUID + "> please look",
			want: true,
		},
		{
			name:     "thread reply, no participation → false",
			text:     "non-mention reply",
			threadTS: threadTS,
			want:     false,
		},
		{
			name:     "thread reply, bot is parent sender → true",
			text:     "non-mention reply",
			threadTS: threadTS,
			threadLines: []modelv1.Line{
				msg("P1", botUID, "thread root", nil),
			},
			want: true,
		},
		{
			name:     "thread reply, prior mention in parent raw → true",
			text:     "non-mention reply",
			threadTS: threadTS,
			threadLines: []modelv1.Line{
				msg("P1", userUID, "@Bot kick off", mentionRaw(botUID)),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, s, acct := newTestMessageStore(t)
			for _, line := range tt.threadLines {
				if err := s.AppendThread(acct, channel, threadTS, line); err != nil {
					t.Fatalf("AppendThread: %v", err)
				}
			}
			l := &Listener{
				acct:         acct,
				pigeonBotUID: botUID,
				messages:     ms,
			}
			got := l.shouldRouteInChannel(tt.text, tt.threadTS, channel)
			if got != tt.want {
				t.Errorf("shouldRouteInChannel(%q, %q) = %v, want %v",
					tt.text, tt.threadTS, got, tt.want)
			}
		})
	}
}

// TestShouldRouteInChannel_EmptyBotUID confirms the predicate degrades
// safely (returns false) when the listener has no bot UID configured —
// otherwise an empty-string UID would match the substring "<@>" anywhere.
func TestShouldRouteInChannel_EmptyBotUID(t *testing.T) {
	ms, _, acct := newTestMessageStore(t)
	l := &Listener{
		acct:         acct,
		pigeonBotUID: "",
		messages:     ms,
	}
	if got := l.shouldRouteInChannel("<@> hi", "", "#general"); got {
		t.Errorf("shouldRouteInChannel with empty botUID should return false")
	}
}

func TestShouldRoute(t *testing.T) {
	const channel = "#general"
	mention := "<@" + botUID + "> ping"

	tests := []struct {
		name        string
		channelType string
		texts       []string
		want        bool
	}{
		{name: "im routes regardless of text", channelType: "im", texts: []string{"hi"}, want: true},
		{name: "mpim routes regardless of text", channelType: "mpim", texts: []string{"hi"}, want: true},
		{name: "group (private channel) routes regardless of text", channelType: "group", texts: []string{"hi"}, want: true},
		{name: "channel without mention does not route", channelType: "channel", texts: []string{"hi"}, want: false},
		{name: "channel with mention in only text routes", channelType: "channel", texts: []string{mention}, want: true},
		{name: "channel with mention in any of multiple texts routes (covers edits)", channelType: "channel", texts: []string{"new text", mention}, want: true},
		{name: "channel with no mention in any text does not route", channelType: "channel", texts: []string{"new", "old"}, want: false},
		{name: "unrecognized channel type does not route", channelType: "weird", texts: []string{mention}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, acct := newTestMessageStore(t)
			l := &Listener{acct: acct, pigeonBotUID: botUID, messages: ms}
			got := l.shouldRoute(context.Background(), tt.channelType, channel, "", "test", tt.texts...)
			if got != tt.want {
				t.Errorf("shouldRoute(%q, texts=%v) = %v, want %v",
					tt.channelType, tt.texts, got, tt.want)
			}
		})
	}
}
