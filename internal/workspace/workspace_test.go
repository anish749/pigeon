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

func TestGetCurrentWorkspace_IncludesLinear(t *testing.T) {
	cfg := &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			"work": {Slack: []string{"acme-corp"}, Linear: []string{"eng"}},
		},
	}

	ws, err := GetCurrentWorkspace(cfg, "work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []account.Account{
		account.New("slack", "acme-corp"),
		account.New("linear-issues", "eng"),
	}
	if len(ws.Accounts) != len(want) {
		t.Fatalf("got %d accounts, want %d: %v", len(ws.Accounts), len(want), ws.Accounts)
	}
	for i, got := range ws.Accounts {
		if got != want[i] {
			t.Errorf("Accounts[%d] = %v, want %v", i, got, want[i])
		}
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

func TestContains(t *testing.T) {
	ws := &Workspace{
		Name: "work",
		Accounts: []account.Account{
			account.New("slack", "acme-corp"),
			account.New("whatsapp", "+15551234567"),
		},
	}

	tests := []struct {
		acct account.Account
		want bool
	}{
		{account.New("slack", "acme-corp"), true},
		{account.New("whatsapp", "+15551234567"), true},
		{account.New("slack", "other-org"), false},
		{account.New("gws", "acme-corp"), false},
	}
	for _, tt := range tests {
		if got := ws.Contains(tt.acct); got != tt.want {
			t.Errorf("Contains(%s) = %v, want %v", tt.acct.Display(), got, tt.want)
		}
	}
}

func TestAccountsForPlatform(t *testing.T) {
	ws := &Workspace{
		Name: "work",
		Accounts: []account.Account{
			account.New("slack", "acme-corp"),
			account.New("slack", "side-project"),
			account.New("whatsapp", "+15551234567"),
		},
	}

	// Filter to slack — should get 2.
	slack := ws.AccountsForPlatform("slack")
	if len(slack) != 2 {
		t.Errorf("AccountsForPlatform(slack) = %d accounts, want 2", len(slack))
	}

	// Filter to whatsapp — should get 1.
	wa := ws.AccountsForPlatform("whatsapp")
	if len(wa) != 1 {
		t.Errorf("AccountsForPlatform(whatsapp) = %d accounts, want 1", len(wa))
	}

	// Filter to gws — should get 0.
	gws := ws.AccountsForPlatform("gws")
	if len(gws) != 0 {
		t.Errorf("AccountsForPlatform(gws) = %d accounts, want 0", len(gws))
	}

	// Empty platform — should get all.
	all := ws.AccountsForPlatform("")
	if len(all) != 3 {
		t.Errorf("AccountsForPlatform(\"\") = %d accounts, want 3", len(all))
	}
}
