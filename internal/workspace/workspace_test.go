package workspace

import (
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
)

func TestGetCurrentWorkspace_FlagOverride(t *testing.T) {
	cfg := &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}, GWS: []string{"work@co.com"}},
		},
	}

	ws, err := GetCurrentWorkspace(cfg, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "work" {
		t.Errorf("Name = %q, want %q", ws.Name, "work")
	}
	want := []account.Account{
		account.New("slack", "acme-corp"),
		account.New("gws", "work@co.com"),
	}
	if len(ws.Accounts) != len(want) {
		t.Fatalf("got %d accounts, want %d", len(ws.Accounts), len(want))
	}
	for i, got := range ws.Accounts {
		if got != want[i] {
			t.Errorf("Accounts[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestGetCurrentWorkspace_FlagOverrideUnknown(t *testing.T) {
	cfg := &config.Config{}

	_, err := GetCurrentWorkspace(cfg, "nope")
	if err == nil {
		t.Fatal("expected error for unknown workspace")
	}
}

func TestGetCurrentWorkspace_EnvVar(t *testing.T) {
	cfg := &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"personal": {WhatsApp: []string{"+15551234567"}},
		},
	}

	t.Setenv(EnvWorkspace, "personal")

	ws, err := GetCurrentWorkspace(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "personal" {
		t.Errorf("Name = %q, want %q", ws.Name, "personal")
	}
	if len(ws.Accounts) != 1 || ws.Accounts[0] != account.New("whatsapp", "+15551234567") {
		t.Errorf("Accounts = %v, want [whatsapp/+15551234567]", ws.Accounts)
	}
}

func TestGetCurrentWorkspace_EnvVarUnknown(t *testing.T) {
	cfg := &config.Config{}

	t.Setenv(EnvWorkspace, "nope")

	_, err := GetCurrentWorkspace(cfg, "")
	if err == nil {
		t.Fatal("expected error for unknown workspace from env")
	}
}

func TestGetCurrentWorkspace_FlagTakesPrecedenceOverEnv(t *testing.T) {
	cfg := &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work":     {Slack: []string{"acme-corp"}},
			"personal": {Slack: []string{"side-project"}},
		},
	}

	t.Setenv(EnvWorkspace, "personal")

	ws, err := GetCurrentWorkspace(cfg, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "work" {
		t.Errorf("Name = %q, want %q", ws.Name, "work")
	}
}

func TestGetCurrentWorkspace_ConfigDefault(t *testing.T) {
	cfg := &config.Config{
		DefaultWorkspace: "work",
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {GWS: []string{"work@co.com"}},
		},
	}

	ws, err := GetCurrentWorkspace(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "work" {
		t.Errorf("Name = %q, want %q", ws.Name, "work")
	}
}

func TestGetCurrentWorkspace_ConfigDefaultUnknown(t *testing.T) {
	cfg := &config.Config{
		DefaultWorkspace: "nope",
	}

	_, err := GetCurrentWorkspace(cfg, "")
	if err == nil {
		t.Fatal("expected error for unknown default workspace")
	}
}

func TestGetCurrentWorkspace_NoWorkspaceReturnsAll(t *testing.T) {
	cfg := &config.Config{
		Slack:    []config.SlackConfig{{Workspace: "acme-corp"}},
		GWS:      []config.GWSConfig{{Email: "work@co.com"}},
		WhatsApp: []config.WhatsAppConfig{{Account: "phone1"}},
		Linear:   []config.LinearConfig{{Workspace: "eng"}},
	}

	ws, err := GetCurrentWorkspace(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws.Name != "" {
		t.Errorf("Name = %q, want empty", ws.Name)
	}
	if len(ws.Accounts) != 4 {
		t.Fatalf("got %d accounts, want 4: %v", len(ws.Accounts), ws.Accounts)
	}
}

func TestGetCurrentWorkspace_NoAccountsConfigured(t *testing.T) {
	cfg := &config.Config{}

	ws, err := GetCurrentWorkspace(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ws.Accounts) != 0 {
		t.Errorf("got %d accounts, want 0", len(ws.Accounts))
	}
}
