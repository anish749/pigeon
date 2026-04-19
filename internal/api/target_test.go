package api

import (
	"strings"
	"testing"
)

func TestSlackTargetValidate(t *testing.T) {
	tests := []struct {
		name    string
		target  SlackTarget
		wantErr string
	}{
		{name: "valid user_id", target: SlackTarget{UserID: "U07HF6KQ7PY"}, wantErr: ""},
		{name: "valid channel", target: SlackTarget{Channel: "#engineering"}, wantErr: ""},
		{name: "valid mpdm", target: SlackTarget{Channel: "@mpdm-alice--bob-1"}, wantErr: ""},
		{name: "both set", target: SlackTarget{UserID: "U123", Channel: "#eng"}, wantErr: "specify user_id or channel, not both"},
		{name: "neither set", target: SlackTarget{}, wantErr: "specify user_id or channel"},
		{name: "bad user_id prefix", target: SlackTarget{UserID: "C07HF6KQ7PY"}, wantErr: "U-prefixed"},
		{name: "@ channel not mpdm", target: SlackTarget{Channel: "@alice"}, wantErr: "use user_id for DMs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Fatalf("error %q does not contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestSlackTargetDisplay(t *testing.T) {
	tests := []struct {
		name   string
		target SlackTarget
		want   string
	}{
		{name: "user_id", target: SlackTarget{UserID: "U123"}, want: "U123"},
		{name: "channel", target: SlackTarget{Channel: "#eng"}, want: "#eng"},
		{name: "both prefers user_id", target: SlackTarget{UserID: "U123", Channel: "#eng"}, want: "U123"},
		{name: "empty", target: SlackTarget{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.target.Display(); got != tt.want {
				t.Fatalf("Display() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlackTargetDisplayWithName(t *testing.T) {
	tests := []struct {
		name         string
		target       SlackTarget
		resolvedName string
		want         string
	}{
		{name: "user with name", target: SlackTarget{UserID: "U123"}, resolvedName: "Alice", want: "Alice (U123)"},
		{name: "channel with name", target: SlackTarget{Channel: "#eng"}, resolvedName: "engineering", want: "engineering"},
		{name: "user no name falls back", target: SlackTarget{UserID: "U123"}, resolvedName: "", want: "U123"},
		{name: "channel no name falls back", target: SlackTarget{Channel: "#eng"}, resolvedName: "", want: "#eng"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.target.DisplayWithName(tt.resolvedName); got != tt.want {
				t.Fatalf("DisplayWithName(%q) = %q, want %q", tt.resolvedName, got, tt.want)
			}
		})
	}
}

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		slack    *SlackTarget
		contact  string
		wantErr  string
	}{
		{name: "slack with user_id", platform: "slack", slack: &SlackTarget{UserID: "U123"}, wantErr: ""},
		{name: "slack with channel", platform: "slack", slack: &SlackTarget{Channel: "#eng"}, wantErr: ""},
		{name: "whatsapp with contact", platform: "whatsapp", contact: "Alice", wantErr: ""},
		{name: "no target", platform: "slack", wantErr: "specify a target"},
		{name: "both set", platform: "slack", slack: &SlackTarget{UserID: "U123"}, contact: "Alice", wantErr: "not both"},
		{name: "slack with contact only", platform: "slack", contact: "Alice", wantErr: "use slack target"},
		{name: "whatsapp with slack target", platform: "whatsapp", slack: &SlackTarget{UserID: "U123"}, wantErr: "use contact for WhatsApp"},
		{name: "slack validates inner", platform: "slack", slack: &SlackTarget{UserID: "C123"}, wantErr: "U-prefixed"},
		{name: "slack empty struct", platform: "slack", slack: &SlackTarget{}, wantErr: "specify user_id or channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTarget(tt.platform, tt.slack, tt.contact)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Fatalf("error %q does not contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestSendRequestTarget(t *testing.T) {
	tests := []struct {
		name string
		req  SendRequest
		want string
	}{
		{name: "slack user", req: SendRequest{Slack: &SlackTarget{UserID: "U123"}}, want: "U123"},
		{name: "slack channel", req: SendRequest{Slack: &SlackTarget{Channel: "#eng"}}, want: "#eng"},
		{name: "whatsapp contact", req: SendRequest{Contact: "Alice"}, want: "Alice"},
		{name: "empty", req: SendRequest{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.Target(); got != tt.want {
				t.Fatalf("Target() = %q, want %q", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
