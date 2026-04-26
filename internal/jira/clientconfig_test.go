package jira

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	t.Setenv(jiraConfigEnv, "")
	t.Setenv(jiraXDGConfigEnv, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	// Default with no XDG_CONFIG_HOME: $HOME/.config/.jira/.config.yml.
	wantDefault := filepath.Join(home, ".config", ".jira", ".config.yml")
	got, err := ResolveConfigPath("")
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if got != wantDefault {
		t.Errorf("default = %q, want %q", got, wantDefault)
	}

	// XDG_CONFIG_HOME overrides $HOME/.config but not the rest.
	t.Setenv(jiraXDGConfigEnv, "/tmp/xdg")
	wantXDG := "/tmp/xdg/.jira/.config.yml"
	got, err = ResolveConfigPath("")
	if err != nil {
		t.Fatalf("XDG default: %v", err)
	}
	if got != wantXDG {
		t.Errorf("XDG default = %q, want %q", got, wantXDG)
	}
	t.Setenv(jiraXDGConfigEnv, "")

	// JIRA_CONFIG_FILE env beats default.
	t.Setenv(jiraConfigEnv, "/tmp/custom.yml")
	got, err = ResolveConfigPath("")
	if err != nil {
		t.Fatalf("env override: %v", err)
	}
	if got != "/tmp/custom.yml" {
		t.Errorf("env override = %q", got)
	}

	// Explicit override beats env.
	got, err = ResolveConfigPath("/explicit.yml")
	if err != nil {
		t.Fatalf("explicit override: %v", err)
	}
	if got != "/explicit.yml" {
		t.Errorf("explicit override = %q", got)
	}

	// Tilde expansion in explicit override.
	t.Setenv(jiraConfigEnv, "")
	got, err = ResolveConfigPath("~/foo/bar.yml")
	if err != nil {
		t.Fatalf("tilde: %v", err)
	}
	want := filepath.Join(home, "foo/bar.yml")
	if got != want {
		t.Errorf("tilde = %q, want %q", got, want)
	}
}
