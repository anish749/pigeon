package jira

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/jira/poller"
)

// sampleCloudYAML is the shape `jira init` produces for an Atlassian Cloud
// install. Includes the discovered fields pigeon ignores (project.key,
// board, epic, issue.types, issue.fields.custom) so the test exercises the
// real parse path, not a stripped-down one.
const sampleCloudYAML = `
installation: Cloud
server: https://acme.atlassian.net
login: alice@acme.com
auth_type: basic
insecure: false
timezone: UTC
project:
  key: ENG
  type: software
board:
  id: 12
  name: ENG board
  type: scrum
epic:
  name: customfield_10011
  link: customfield_10014
issue:
  types:
    - name: Bug
      handle: Bug
      id: "10001"
      subtask: false
  fields:
    custom:
      - name: Story Points
        key: customfield_10016
        schema: {datatype: number}
`

func TestLoadClientConfigCloud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	if err := os.WriteFile(path, []byte(sampleCloudYAML), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JIRA_API_TOKEN", "fake-token-xyz")

	cfg, ver, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig: %v", err)
	}
	if cfg.Server != "https://acme.atlassian.net" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Login != "alice@acme.com" {
		t.Errorf("Login = %q", cfg.Login)
	}
	if cfg.APIToken != "fake-token-xyz" {
		t.Error("APIToken not sourced from env")
	}
	if cfg.AuthType == nil || string(*cfg.AuthType) != "basic" {
		t.Errorf("AuthType = %v", cfg.AuthType)
	}
	if ver != poller.APIVersionV3 {
		t.Errorf("ver = %v, want APIVersionV3 (Cloud)", ver)
	}
}

func TestLoadClientConfigLocal(t *testing.T) {
	yamlBody := `installation: local
server: https://jira.internal.example.com
login: alice
auth_type: bearer
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	if err := os.WriteFile(path, []byte(yamlBody), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JIRA_API_TOKEN", "tok")

	cfg, ver, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig: %v", err)
	}
	if ver != poller.APIVersionV2 {
		t.Errorf("ver = %v, want APIVersionV2 (Server)", ver)
	}
	if cfg.AuthType == nil || string(*cfg.AuthType) != "bearer" {
		t.Errorf("AuthType = %v", cfg.AuthType)
	}
}

func TestLoadClientConfigMissingToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	_ = os.WriteFile(path, []byte(sampleCloudYAML), 0600)
	t.Setenv("JIRA_API_TOKEN", "")

	if _, _, err := LoadClientConfig(path); err == nil {
		t.Error("expected error when JIRA_API_TOKEN is unset")
	}
}

func TestLoadClientConfigMissingFile(t *testing.T) {
	t.Setenv("JIRA_API_TOKEN", "tok")
	if _, _, err := LoadClientConfig("/nonexistent/.config.yml"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadClientConfigMissingServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	if err := os.WriteFile(path, []byte("login: alice"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JIRA_API_TOKEN", "tok")

	if _, _, err := LoadClientConfig(path); err == nil {
		t.Error("expected error when server is missing")
	}
}

func TestResolveConfigPath(t *testing.T) {
	t.Setenv(jiraConfigEnv, "")

	// Default: ~/.config/.jira/.config.yml
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
