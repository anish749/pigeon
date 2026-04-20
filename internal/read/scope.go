package read

import (
	"fmt"

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
	root := paths.DefaultDataRoot()

	// No workspace — fall back to single-directory behavior.
	if ws == nil {
		return []string{searchDir(root, platform, accountName)}, nil
	}

	// Explicit account — validate it's in the workspace.
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		if !ws.Contains(acct) {
			return nil, fmt.Errorf("account %s is not in workspace %q", acct.Display(), ws.Name)
		}
		return []string{root.AccountFor(acct).Path()}, nil
	}

	accounts := ws.AccountsForPlatform(platform)
	if len(accounts) == 0 {
		if platform != "" {
			return nil, fmt.Errorf("workspace %q has no %s accounts", ws.Name, platform)
		}
		return nil, fmt.Errorf("workspace %q has no accounts", ws.Name)
	}

	dirs := make([]string, len(accounts))
	for i, acct := range accounts {
		dirs[i] = root.AccountFor(acct).Path()
	}
	return dirs, nil
}

// searchDir returns a single data directory scoped by optional platform and
// account filters.
func searchDir(root paths.DataRoot, platform, accountName string) string {
	switch {
	case platform != "" && accountName != "":
		return root.AccountFor(account.New(platform, accountName)).Path()
	case platform != "":
		return root.Platform(platform).Path()
	default:
		return root.Path()
	}
}
