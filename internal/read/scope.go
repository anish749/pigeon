package read

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/workspace"
)

// SearchDirs returns the data directories to search, scoped by the active
// workspace and any explicit platform/account flags.
//
// The workspace is a hard boundary — explicit platform/account flags narrow
// within the workspace but cannot escape it. An explicit account that is not
// in the workspace is rejected.
//
// With no active workspace (ws == nil), returns a single directory based on
// the platform/account filters: account dir, platform dir, or data root.
func SearchDirs(ws *workspace.Workspace, platform, accountName string) ([]string, error) {
	// No workspace — fall back to single-directory behavior.
	if ws == nil {
		dir := paths.SearchDir(platform, accountName)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, fmt.Errorf("no data at %s", dir)
		}
		return []string{dir}, nil
	}

	root := paths.DefaultDataRoot()

	// Explicit account — validate it's in the workspace.
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		if !ws.Contains(acct) {
			return nil, fmt.Errorf("account %s is not in workspace %q", acct.Display(), ws.Name)
		}
		dir := root.AccountFor(acct).Path()
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, fmt.Errorf("no data for %s", acct.Display())
		}
		return []string{dir}, nil
	}

	accounts := ws.AccountsForPlatform(platform)
	if len(accounts) == 0 {
		if platform != "" {
			return nil, fmt.Errorf("workspace %q has no %s accounts", ws.Name, platform)
		}
		return nil, fmt.Errorf("workspace %q has no accounts", ws.Name)
	}

	var dirs []string
	for _, acct := range accounts {
		dir := root.AccountFor(acct).Path()
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			slog.Info("skipping account with no data", "account", acct.Display(), "workspace", ws.Name)
			continue
		}
		dirs = append(dirs, dir)
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no synced data for any account in workspace %q", ws.Name)
	}
	return dirs, nil
}
