package jira

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	t.Setenv(jiraConfigEnv, "")

	// Default: ~/.jira/.config.yml
	got := ResolveConfigPath("")
	home, _ := os.UserHomeDir()
	wantDefault := filepath.Join(home, ".jira", ".config.yml")
	if got != wantDefault {
		t.Errorf("default = %q, want %q", got, wantDefault)
	}

	// Env override.
	t.Setenv(jiraConfigEnv, "/tmp/custom.yml")
	if got := ResolveConfigPath(""); got != "/tmp/custom.yml" {
		t.Errorf("env override = %q", got)
	}

	// Explicit override beats env.
	if got := ResolveConfigPath("/explicit.yml"); got != "/explicit.yml" {
		t.Errorf("explicit override = %q", got)
	}

	// Tilde expansion.
	t.Setenv(jiraConfigEnv, "")
	got = ResolveConfigPath("~/foo/bar.yml")
	want := filepath.Join(home, "foo/bar.yml")
	if got != want {
		t.Errorf("tilde = %q, want %q", got, want)
	}
}
