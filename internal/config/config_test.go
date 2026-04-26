package config

import "testing"

func TestSlackConfigAppDisplay(t *testing.T) {
	tests := []struct {
		name string
		cfg  SlackConfig
		want string
	}{
		{name: "default", cfg: SlackConfig{}, want: "Pigeon"},
		{name: "configured", cfg: SlackConfig{AppDisplayName: "Owl"}, want: "Owl"},
		{name: "trimmed", cfg: SlackConfig{AppDisplayName: "  Owl  "}, want: "Owl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.AppDisplay(); got != tt.want {
				t.Fatalf("AppDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlackConfigAppAttribution(t *testing.T) {
	tests := []struct {
		name string
		cfg  SlackConfig
		want string
	}{
		{name: "default", cfg: SlackConfig{}, want: "pigeon"},
		{name: "configured preserves case", cfg: SlackConfig{AppDisplayName: "Owl"}, want: "Owl"},
		{name: "trimmed", cfg: SlackConfig{AppDisplayName: "  owl  "}, want: "owl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.AppAttribution(); got != tt.want {
				t.Fatalf("AppAttribution() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddSlackPreservesAppDisplayName(t *testing.T) {
	cfg := &Config{}
	cfg.AddSlack(SlackConfig{Workspace: "acme", TeamID: "T1", AppDisplayName: "Owl", BotToken: "old"})
	cfg.AddSlack(SlackConfig{Workspace: "acme", TeamID: "T1", BotToken: "new"})

	if got := cfg.Slack[0].AppDisplayName; got != "Owl" {
		t.Fatalf("AppDisplayName = %q, want %q", got, "Owl")
	}
	if got := cfg.Slack[0].BotToken; got != "new" {
		t.Fatalf("BotToken = %q, want %q", got, "new")
	}
}
