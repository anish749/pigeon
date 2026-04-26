package api

import "testing"

func TestSlackSenderAppName(t *testing.T) {
	tests := []struct {
		name   string
		sender *SlackSender
		want   string
	}{
		{name: "nil defaults lowercase", sender: nil, want: "pigeon"},
		{name: "empty defaults lowercase", sender: &SlackSender{}, want: "pigeon"},
		{name: "configured preserves case", sender: &SlackSender{AppName: "Owl"}, want: "Owl"},
		{name: "configured lowercase", sender: &SlackSender{AppName: "owl"}, want: "owl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sender.appName(); got != tt.want {
				t.Fatalf("appName() = %q, want %q", got, tt.want)
			}
		})
	}
}
