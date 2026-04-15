package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
)

func TestRunUnlinkSlack(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	dataDir := filepath.Join(tmpDir, "data")
	t.Setenv("PIGEON_CONFIG_DIR", configDir)
	t.Setenv("PIGEON_DATA_DIR", dataDir)

	// Seed config with two workspaces.
	cfg := &config.Config{}
	cfg.AddSlack(config.SlackConfig{Workspace: "acme-corp", TeamID: "T1", BotToken: "xoxb-1"})
	cfg.AddSlack(config.SlackConfig{Workspace: "other-corp", TeamID: "T2", BotToken: "xoxb-2"})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Create some message data for acme-corp.
	root := paths.NewDataRoot(dataDir)
	acct := account.New("slack", "acme-corp")
	acctDir := root.AccountFor(acct).Path()
	chanDir := filepath.Join(acctDir, "#general")
	if err := os.MkdirAll(chanDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chanDir, "2026-04-15.jsonl"), []byte(`{"type":"msg"}`+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Unlink acme-corp.
	if err := RunUnlinkSlack("acme-corp"); err != nil {
		t.Fatalf("RunUnlinkSlack: %v", err)
	}

	// Data directory should be gone.
	if _, err := os.Stat(acctDir); !os.IsNotExist(err) {
		t.Errorf("expected data dir to be deleted, got err=%v", err)
	}

	// Config should only have other-corp.
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(reloaded.Slack) != 1 {
		t.Fatalf("expected 1 Slack entry, got %d", len(reloaded.Slack))
	}
	if reloaded.Slack[0].Workspace != "other-corp" {
		t.Errorf("expected remaining workspace to be other-corp, got %s", reloaded.Slack[0].Workspace)
	}
}

func TestRunUnlinkSlack_SingleAccount(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("PIGEON_DATA_DIR", filepath.Join(tmpDir, "data"))

	cfg := &config.Config{}
	cfg.AddSlack(config.SlackConfig{Workspace: "only-one", TeamID: "T1", BotToken: "xoxb-1"})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// No --account flag needed when there's only one.
	if err := RunUnlinkSlack(""); err != nil {
		t.Fatalf("RunUnlinkSlack: %v", err)
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(reloaded.Slack) != 0 {
		t.Errorf("expected 0 Slack entries, got %d", len(reloaded.Slack))
	}
}

func TestRunUnlinkSlack_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("PIGEON_DATA_DIR", filepath.Join(tmpDir, "data"))

	cfg := &config.Config{}
	cfg.AddSlack(config.SlackConfig{Workspace: "acme-corp", TeamID: "T1"})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := RunUnlinkSlack("no-such-workspace")
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
}

func TestRunUnlinkSlack_MultipleRequiresAccount(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("PIGEON_DATA_DIR", filepath.Join(tmpDir, "data"))

	cfg := &config.Config{}
	cfg.AddSlack(config.SlackConfig{Workspace: "ws-a", TeamID: "T1"})
	cfg.AddSlack(config.SlackConfig{Workspace: "ws-b", TeamID: "T2"})
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := RunUnlinkSlack("")
	if err == nil {
		t.Fatal("expected error when multiple accounts and no --account")
	}
}

func TestRunUnlinkSlack_NoAccounts(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PIGEON_CONFIG_DIR", filepath.Join(tmpDir, "config"))
	t.Setenv("PIGEON_DATA_DIR", filepath.Join(tmpDir, "data"))

	cfg := &config.Config{}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := RunUnlinkSlack("")
	if err == nil {
		t.Fatal("expected error when no accounts configured")
	}
}
