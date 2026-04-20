package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/workspace"
)

// resolveSearchDirs returns the data directories to search, scoped by the
// active workspace and any explicit platform/account flags.
//
// The workspace is a hard boundary — explicit platform/account flags narrow
// within the workspace but cannot escape it. An explicit account that is not
// in the workspace is rejected.
//
// With no active workspace, falls back to paths.SearchDir (single directory).
func resolveSearchDirs(cmd *cobra.Command, platform, accountName string) ([]string, error) {
	ws, err := currentWorkspace(cmd)
	if err != nil {
		return nil, err
	}

	// No workspace — fall back to single-directory behavior.
	if ws == nil {
		return []string{paths.SearchDir(platform, accountName)}, nil
	}

	// Explicit account — validate it's in the workspace.
	if platform != "" && accountName != "" {
		acct := account.New(platform, accountName)
		if !ws.Contains(acct) {
			return nil, fmt.Errorf("account %s is not in workspace %q", acct.Display(), ws.Name)
		}
		return []string{paths.SearchDir(platform, accountName)}, nil
	}

	accounts := ws.AccountsForPlatform(platform)
	if len(accounts) == 0 {
		if platform != "" {
			return nil, fmt.Errorf("workspace %q has no %s accounts", ws.Name, platform)
		}
		return nil, fmt.Errorf("workspace %q has no accounts", ws.Name)
	}

	root := paths.DefaultDataRoot()
	dirs := make([]string, len(accounts))
	for i, acct := range accounts {
		dirs[i] = root.AccountFor(acct).Path()
	}
	return dirs, nil
}

// currentWorkspace resolves the active workspace from the --workspace flag,
// environment, or config default. Returns nil when no workspace is active.
func currentWorkspace(cmd *cobra.Command) (*workspace.Workspace, error) {
	wsFlag, _ := cmd.Flags().GetString("workspace")
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	ws, err := workspace.GetCurrentWorkspace(cfg, wsFlag)
	if err != nil {
		return nil, err
	}
	// GetCurrentWorkspace returns a workspace with no name when no workspace
	// is configured — treat that as "no workspace".
	if ws.Name == "" {
		return nil, nil
	}
	return ws, nil
}

// validateAccountInWorkspace checks that the given account is within the
// active workspace. Returns nil if no workspace is active or the account
// is in scope.
func validateAccountInWorkspace(cmd *cobra.Command, acct account.Account) error {
	ws, err := currentWorkspace(cmd)
	if err != nil {
		return err
	}
	if ws == nil {
		return nil
	}
	if !ws.Contains(acct) {
		return fmt.Errorf("account %s is not in workspace %q", acct.Display(), ws.Name)
	}
	return nil
}
