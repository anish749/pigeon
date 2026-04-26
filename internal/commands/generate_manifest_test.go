package commands

import (
	"strings"
	"testing"

	"github.com/anish749/pigeon/internal/config"
)

func TestRenderManifestTemplateUsesAppDisplayName(t *testing.T) {
	tmpl := `name: "${USERNAME}'s ${WORKSPACE_NAME} ${APP_DISPLAY_NAME}"`

	got := renderManifestTemplate(tmpl, "Anish", "acme", "Owl")
	want := `name: "Anish's acme Owl"`
	if got != want {
		t.Fatalf("renderManifestTemplate() = %q, want %q", got, want)
	}
}

func TestResolveSlackAppDisplayName(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Slack: []config.SlackConfig{
			{Workspace: "acme", TeamID: "T1", AppDisplayName: "Owl"},
		},
	})

	tests := []struct {
		name      string
		workspace string
		override  string
		want      string
	}{
		{name: "override wins", workspace: "acme", override: "Falcon", want: "Falcon"},
		{name: "workspace config", workspace: "acme", want: "Owl"},
		{name: "default", workspace: "other", want: "Pigeon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSlackAppDisplayName(tt.workspace, tt.override)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolveSlackAppDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfiguredSlackAppDisplayNameIgnoresEmptyDefault(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Slack: []config.SlackConfig{
			{Workspace: "acme", TeamID: "T1"},
		},
	})

	got, ok, err := configuredSlackAppDisplayName("acme")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("configuredSlackAppDisplayName() ok = true, got name %q", got)
	}
}

func TestRenderManifestTemplateReplacesAllPublicAppNames(t *testing.T) {
	got := renderManifestTemplate(slackManifestSnippet, "Anish", "acme", "Owl")
	if strings.Contains(got, "Pigeon") || strings.Contains(got, "pigeon") {
		t.Fatalf("rendered manifest still contains pigeon copy:\n%s", got)
	}
	if count := strings.Count(got, "Owl"); count != 4 {
		t.Fatalf("rendered manifest contains Owl %d times, want 4:\n%s", count, got)
	}
}

const slackManifestSnippet = `display_information:
  name: "${USERNAME}'s ${WORKSPACE_NAME} ${APP_DISPLAY_NAME}"
  description: "${APP_DISPLAY_NAME} carries messages to ${USERNAME} without posting on its own."
  long_description: >-
    This is ${USERNAME}'s personal ${APP_DISPLAY_NAME} for the
    ${WORKSPACE_NAME} workspace.
features:
  bot_user:
    display_name: "${USERNAME}'s ${WORKSPACE_NAME} ${APP_DISPLAY_NAME}"`
