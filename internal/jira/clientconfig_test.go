package jira

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	t.Setenv(jiraConfigEnv, "")
	t.Setenv(jiraXDGConfigEnv, "")
	home, _ := os.UserHomeDir()

	// Default with no XDG_CONFIG_HOME: $HOME/.config/.jira/.config.yml.
	wantDefault := filepath.Join(home, ".config", ".jira", ".config.yml")
	if got := ResolveConfigPath(""); got != wantDefault {
		t.Errorf("default = %q, want %q", got, wantDefault)
	}

	// XDG_CONFIG_HOME overrides $HOME/.config but not the rest.
	t.Setenv(jiraXDGConfigEnv, "/tmp/xdg")
	wantXDG := "/tmp/xdg/.jira/.config.yml"
	if got := ResolveConfigPath(""); got != wantXDG {
		t.Errorf("XDG default = %q, want %q", got, wantXDG)
	}
	t.Setenv(jiraXDGConfigEnv, "")

	// JIRA_CONFIG_FILE env beats default.
	t.Setenv(jiraConfigEnv, "/tmp/custom.yml")
	if got := ResolveConfigPath(""); got != "/tmp/custom.yml" {
		t.Errorf("env override = %q", got)
	}

	// Explicit override beats env.
	if got := ResolveConfigPath("/explicit.yml"); got != "/explicit.yml" {
		t.Errorf("explicit override = %q", got)
	}

	// Tilde expansion in explicit override.
	t.Setenv(jiraConfigEnv, "")
	got := ResolveConfigPath("~/foo/bar.yml")
	want := filepath.Join(home, "foo/bar.yml")
	if got != want {
		t.Errorf("tilde = %q, want %q", got, want)
	}
}
