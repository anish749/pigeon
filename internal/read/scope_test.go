package read

import (
	"testing"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workspace"
)

func TestSearchDirs_NoWorkspace(t *testing.T) {
	dirs, err := SearchDirs(nil, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
}

func TestSearchDirs_NoWorkspaceWithPlatformAndAccount(t *testing.T) {
	dirs, err := SearchDirs(nil, "slack", "acme-corp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1", len(dirs))
	}
}

func TestSearchDirs_WorkspaceReturnsAccountDirs(t *testing.T) {
	ws := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack:    []string{"acme-corp"},
		WhatsApp: []string{"+15551234567"},
	})

	dirs, err := SearchDirs(ws, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2: %v", len(dirs), dirs)
	}
}

func TestSearchDirs_WorkspaceFiltersByPlatform(t *testing.T) {
	ws := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack:    []string{"acme-corp"},
		WhatsApp: []string{"+15551234567"},
	})

	dirs, err := SearchDirs(ws, "slack", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1: %v", len(dirs), dirs)
	}
}

func TestSearchDirs_WorkspacePlatformNotPresent(t *testing.T) {
	ws := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack: []string{"acme-corp"},
	})

	_, err := SearchDirs(ws, "whatsapp", "")
	if err == nil {
		t.Fatal("expected error for platform not in workspace")
	}
}

func TestSearchDirs_ExplicitAccountInWorkspace(t *testing.T) {
	ws := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack: []string{"acme-corp"},
	})

	dirs, err := SearchDirs(ws, "slack", "acme-corp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1: %v", len(dirs), dirs)
	}
}

func TestSearchDirs_ExplicitAccountNotInWorkspace(t *testing.T) {
	ws := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack: []string{"acme-corp"},
	})

	_, err := SearchDirs(ws, "slack", "other-org")
	if err == nil {
		t.Fatal("expected error for account not in workspace")
	}
}

func TestSearchDirs_EmptyWorkspace(t *testing.T) {
	ws := &workspace.Workspace{Name: "empty"}

	_, err := SearchDirs(ws, "", "")
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
}

// makeWorkspace is a test helper that resolves a workspace config into a Workspace.
func makeWorkspace(t *testing.T, name string, wsCfg config.WorkspaceConfig) *workspace.Workspace {
	t.Helper()
	cfg := &config.Config{
		Workspaces: map[config.WorkspaceName]config.WorkspaceConfig{
			config.WorkspaceName(name): wsCfg,
		},
	}
	ws, err := workspace.GetCurrentWorkspace(cfg, name)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	return ws
}
