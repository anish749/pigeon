package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workspace"
)

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
