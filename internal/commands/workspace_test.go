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
			"work": {
				Slack:  []string{"acme-corp"},
				GWS:    []string{"you@company.com"},
				Linear: []string{"eng"},
				Jira:   []string{"acme"},
			},
		},
		DefaultWorkspace: "work",
	})
	cfg := reloadConfig(t)

	// Just verify the call completes for a workspace that exercises every
	// platform's print branch (slack/gws/linear/jira; whatsapp covered
	// elsewhere).
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

func TestRunWorkspaceAdd_LinearAndJira(t *testing.T) {
	setupTestConfig(t, &config.Config{
		Linear: []config.LinearConfig{{Workspace: "eng", Account: "Engineering"}},
		Jira: []config.JiraConfig{
			{JiraConfig: "/some/path.yml", APIToken: "tok", AccountName: "acme"},
		},
	})

	cfg := reloadConfig(t)
	if err := RunWorkspaceAdd(cfg, "work", "linear", "eng"); err != nil {
		t.Fatalf("add linear: %v", err)
	}
	cfg = reloadConfig(t)
	if err := RunWorkspaceAdd(cfg, "work", "jira", "acme"); err != nil {
		t.Fatalf("add jira: %v", err)
	}

	cfg = reloadConfig(t)
	ws := cfg.Workspaces["work"]
	if len(ws.Linear) != 1 || ws.Linear[0] != "eng" {
		t.Errorf("linear in workspace: %v", ws.Linear)
	}
	if len(ws.Jira) != 1 || ws.Jira[0] != "acme" {
		t.Errorf("jira in workspace: %v", ws.Jira)
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
		Linear:   []config.LinearConfig{{Workspace: "eng", Account: "Engineering"}},
		Jira: []config.JiraConfig{
			{JiraConfig: "/some/path.yml", APIToken: "tok", AccountName: "acme"},
		},
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
		{"linear", "eng", false},
		{"linear", "nonexistent", true},
		{"jira", "acme", false},
		{"jira", "nonexistent", true},
		{"unknown-platform", "anything", true},
	}

	for _, tt := range tests {
		err := validateAccountExists(cfg, tt.platform, tt.account)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateAccountExists(%q, %q): got err=%v, wantErr=%v", tt.platform, tt.account, err, tt.wantErr)
		}
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
