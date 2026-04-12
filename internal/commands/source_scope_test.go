package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveScope_ContextOverrideBeatsEnv(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", configDir)
	t.Setenv("PIGEON_DATA_DIR", dataDir)
	t.Setenv("PIGEON_CONTEXT", "personal")

	configYAML := []byte(`
gws:
  - account: work
    email: work@company.com
  - account: personal
    email: me@example.com
contexts:
  work:
    gws: work@company.com
  personal:
    gws: me@example.com
default_context: personal
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	scope, err := ResolveScope("gmail", "work", "")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got, want := scope.ContextName, "work"; got != want {
		t.Fatalf("scope.ContextName = %q, want %q", got, want)
	}
	if got, want := len(scope.Accounts), 1; got != want {
		t.Fatalf("len(scope.Accounts) = %d, want %d", got, want)
	}
	if got, want := scope.Accounts[0].Identifier, "work@company.com"; got != want {
		t.Fatalf("scope.Accounts[0].Identifier = %q, want %q", got, want)
	}
}

func TestResolveScope_NoAccountFlagReadsAllMatchingAccounts(t *testing.T) {
	configDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", configDir)
	t.Setenv("PIGEON_DATA_DIR", dataDir)

	configYAML := []byte(`
slack:
  - workspace: acme-corp
  - workspace: vendor-ws
contexts:
  work:
    slack: [acme-corp, vendor-ws]
`)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	scope, err := ResolveScope("slack", "work", "")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if got, want := len(scope.Accounts), 2; got != want {
		t.Fatalf("len(scope.Accounts) = %d, want %d", got, want)
	}
}
