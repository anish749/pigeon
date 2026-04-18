package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/config"
)

// setupTestConfig writes a config to a temp dir and sets the env var so
// config.Load/Save use it.
func setupTestConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", dir)

	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// reloadConfig loads the config from the test dir.
func reloadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunWorkspaceList_Empty(t *testing.T) {
	setupTestConfig(t, &config.Config{})
	cfg := reloadConfig(t)

	// Should not error on empty config.
	if err := RunWorkspaceList(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkspaceList_WithWorkspaces(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}, GWS: []string{"you@company.com"}},
		},
		DefaultWorkspace: "work",
	})
	cfg := reloadConfig(t)

	if err := RunWorkspaceList(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkspaceAdd(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Slack: []config.SlackConfig{{Workspace: "acme-corp", TeamID: "T1"}},
		GWS:   []config.GWSConfig{{Email: "you@company.com", Account: "work"}},
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceAdd(cfg, "work", "slack", "acme-corp"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	ws, ok := cfg.Workspaces["work"]
	if !ok {
		t.Fatal("workspace 'work' not created")
	}
	if len(ws.Slack) != 1 || ws.Slack[0] != "acme-corp" {
		t.Fatalf("unexpected slack accounts: %v", ws.Slack)
	}

	// Add a second account to the same workspace.
	if err := RunWorkspaceAdd(cfg, "work", "gws", "you@company.com"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	ws = cfg.Workspaces["work"]
	if len(ws.GWS) != 1 || ws.GWS[0] != "you@company.com" {
		t.Fatalf("unexpected gws accounts: %v", ws.GWS)
	}
}

func TestRunWorkspaceAdd_DuplicateErrors(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Slack: []config.SlackConfig{{Workspace: "acme-corp", TeamID: "T1"}},
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}},
		},
	})
	cfg := reloadConfig(t)

	err := RunWorkspaceAdd(cfg, "work", "slack", "acme-corp")
	if err == nil {
		t.Fatal("expected error for duplicate add")
	}
}

func TestRunWorkspaceAdd_AccountNotConfigured(t *testing.T) {
	setupTestConfig(t, &config.Config{})
	cfg := reloadConfig(t)

	err := RunWorkspaceAdd(cfg, "work", "slack", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unconfigured account")
	}
}

func TestRunWorkspaceRemove(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp", "other-ws"}, GWS: []string{"you@company.com"}},
		},
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceRemove(cfg, "work", "slack", "acme-corp"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	ws := cfg.Workspaces["work"]
	if len(ws.Slack) != 1 || ws.Slack[0] != "other-ws" {
		t.Fatalf("unexpected slack accounts after remove: %v", ws.Slack)
	}
}

func TestRunWorkspaceRemove_LastAccount_DeletesWorkspace(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}},
		},
		DefaultWorkspace: "work",
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceRemove(cfg, "work", "slack", "acme-corp"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	if _, ok := cfg.Workspaces["work"]; ok {
		t.Fatal("workspace should have been deleted when emptied")
	}
	if cfg.DefaultWorkspace != "" {
		t.Fatal("default workspace should have been cleared")
	}
}

func TestRunWorkspaceRemove_NotFound(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}},
		},
	})
	cfg := reloadConfig(t)

	err := RunWorkspaceRemove(cfg, "work", "slack", "nonexistent")
	if err == nil {
		t.Fatal("expected error for removing non-member account")
	}
}

func TestRunWorkspaceDelete(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work":     {Slack: []string{"acme-corp"}},
			"personal": {WhatsApp: []string{"+14155551234"}},
		},
		DefaultWorkspace: "work",
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceDelete(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	if _, ok := cfg.Workspaces["work"]; ok {
		t.Fatal("workspace should have been deleted")
	}
	if _, ok := cfg.Workspaces["personal"]; !ok {
		t.Fatal("personal workspace should still exist")
	}
	if cfg.DefaultWorkspace != "" {
		t.Fatal("default workspace should have been cleared")
	}
}

func TestRunWorkspaceDelete_NotFound(t *testing.T) {
	setupTestConfig(t, &config.Config{})
	cfg := reloadConfig(t)

	err := RunWorkspaceDelete(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting non-existent workspace")
	}
}

func TestRunWorkspaceDefault_Show(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces:       map[config.WorkspaceName]config.WorkspaceConfig{"work": {}},
		DefaultWorkspace: "work",
	})
	cfg := reloadConfig(t)

	if err := RunWorkspaceDefault(cfg, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkspaceDefault_Set(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work":     {Slack: []string{"acme-corp"}},
			"personal": {WhatsApp: []string{"+14155551234"}},
		},
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceDefault(cfg, "personal"); err != nil {
		t.Fatal(err)
	}

	cfg = reloadConfig(t)
	if cfg.DefaultWorkspace != "personal" {
		t.Fatalf("default workspace: got %q, want %q", cfg.DefaultWorkspace, "personal")
	}
}

func TestRunWorkspaceDefault_NotFound(t *testing.T) {
	setupTestConfig(t, &config.Config{})
	cfg := reloadConfig(t)

	err := RunWorkspaceDefault(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent workspace")
	}
}

func TestValidateAccountExists(t *testing.T) {
	cfg := &config.Config{
		Slack:    []config.SlackConfig{{Workspace: "acme-corp", TeamID: "T1"}},
		GWS:      []config.GWSConfig{{Email: "you@company.com", Account: "work"}},
		WhatsApp: []config.WhatsAppConfig{{Account: "+14155551234"}},
	}

	tests := []struct {
		platform string
		account  string
		wantErr  bool
	}{
		{"slack", "acme-corp", false},
		{"slack", "nonexistent", true},
		{"gws", "you@company.com", false},
		{"gws", "bad@email.com", true},
		{"whatsapp", "+14155551234", false},
		{"whatsapp", "+10000000000", true},
		{"linear", "anything", true}, // unsupported
	}

	for _, tt := range tests {
		err := validateAccountExists(cfg, tt.platform, tt.account)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateAccountExists(%q, %q): got err=%v, wantErr=%v", tt.platform, tt.account, err, tt.wantErr)
		}
	}
}

func TestRemoveString(t *testing.T) {
	got, found := removeString([]string{"a", "b", "c"}, "b")
	if !found {
		t.Fatal("expected found=true")
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("unexpected result: %v", got)
	}

	_, found = removeString([]string{"a", "b"}, "x")
	if found {
		t.Fatal("expected found=false for missing element")
	}
}

// Verify the test helper doesn't leave config files outside the temp dir.
func TestSetupTestConfig_UsesTemp(t *testing.T) {
	setupTestConfig(t, &config.Config{})
	path := os.Getenv("PIGEON_CONFIG_DIR")
	if !filepath.IsAbs(path) {
		t.Fatalf("config dir should be absolute, got %q", path)
	}
}
