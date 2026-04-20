package read

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/workspace"
)

func TestSearchDirs(t *testing.T) {
	// Create a temp data root with account directories on disk.
	tmp := t.TempDir()
	t.Setenv("PIGEON_DATA_DIR", tmp)

	// Create account dirs that should be found.
	mkAccountDir(t, tmp, "slack", "acme-corp")
	mkAccountDir(t, tmp, "whatsapp", "+15551234567")

	workWS := makeWorkspace(t, "work", config.WorkspaceConfig{
		Slack:    []string{"acme-corp"},
		WhatsApp: []string{"+15551234567"},
	})
	slackOnlyWS := makeWorkspace(t, "slack-only", config.WorkspaceConfig{
		Slack: []string{"acme-corp"},
	})
	emptyWS := &workspace.Workspace{Name: "empty"}

	tests := []struct {
		name     string
		ws       *workspace.Workspace
		platform string
		account  string
		wantDirs int
		wantErr  bool
	}{
		{"no workspace, no filters", nil, "", "", 1, false},
		{"no workspace, explicit account", nil, "slack", "acme-corp", 1, false},
		{"workspace returns all account dirs", workWS, "", "", 2, false},
		{"workspace filters by platform", workWS, "slack", "", 1, false},
		{"workspace platform not present", slackOnlyWS, "whatsapp", "", 0, true},
		{"explicit account in workspace", slackOnlyWS, "slack", "acme-corp", 1, false},
		{"explicit account not in workspace", slackOnlyWS, "slack", "other-org", 0, true},
		{"empty workspace", emptyWS, "", "", 0, true},
		{"workspace account dir missing on disk", makeWorkspace(t, "ghost", config.WorkspaceConfig{
			Slack: []string{"no-such-org"},
		}), "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs, err := SearchDirs(tt.ws, tt.platform, tt.account)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(dirs) != tt.wantDirs {
				t.Fatalf("got %d dirs, want %d: %v", len(dirs), tt.wantDirs, dirs)
			}
		})
	}
}

// mkAccountDir creates the directory structure for an account on disk.
func mkAccountDir(t *testing.T, root, platform, name string) {
	t.Helper()
	acct := account.New(platform, name)
	dir := paths.NewDataRoot(root).AccountFor(acct).Path()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create account dir: %v", err)
	}
	// Create a dummy file so the dir is non-empty.
	if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
		t.Fatalf("create keep file: %v", err)
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
