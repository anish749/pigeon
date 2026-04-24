package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/tailapi"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "monitor",
		Short:   "Stream incoming messages to stdout",
		GroupID: groupReading,
		Long: `Stream incoming messages and reactions as JSON lines to stdout.
Each line is one event.`,
		Example: `  pigeon monitor
  pigeon monitor --platform=slack --account=acme-corp
  pigeon monitor --workspace=eng --since=5m

  # Filter by kind (output stays as full JSON lines)
  pigeon monitor | grep --line-buffered '"kind":"message"'
  pigeon monitor | grep --line-buffered '"kind":"reaction"'

  # Scope to one conversation or one parent message
  pigeon monitor | jq --unbuffered -c 'select(.conversation == "#engineering")'
  pigeon monitor | jq --unbuffered -c 'select(.kind == "reaction" and .msg_id == "1711568938.123456")'`,
		PreRunE: ensureDaemon,
		RunE:    runMonitor,
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (slack, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "filter by account (requires --platform)")
	cmd.Flags().StringP("since", "s", "", "replay history from this duration ago (e.g. 5m, 2h)")
	return cmd
}

func runMonitor(cmd *cobra.Command, _ []string) error {
	platform, err := cmd.Flags().GetString("platform")
	if err != nil {
		return fmt.Errorf("get platform flag: %w", err)
	}
	acctName, err := cmd.Flags().GetString("account")
	if err != nil {
		return fmt.Errorf("get account flag: %w", err)
	}
	since, err := cmd.Flags().GetString("since")
	if err != nil {
		return fmt.Errorf("get since flag: %w", err)
	}

	req := tailapi.Request{}

	// Workspace resolves at the CLI layer. --platform/--account override
	// the workspace account list if both are set.
	switch {
	case platform != "" && acctName != "":
		if activeWorkspace != nil {
			if err := validateAccountInWorkspace(account.New(platform, acctName)); err != nil {
				return err
			}
		}
		req.Accounts = []account.Account{account.New(platform, acctName)}
	case platform != "":
		if activeWorkspace == nil {
			return fmt.Errorf("--platform without --account requires an active workspace")
		}
		accts := activeWorkspace.AccountsForPlatform(platform)
		if len(accts) == 0 {
			return fmt.Errorf("workspace %q has no %s accounts", activeWorkspace.Name, platform)
		}
		req.Accounts = accts
	case activeWorkspace != nil:
		req.Accounts = activeWorkspace.Accounts
	}

	if since != "" {
		d, err := timeutil.ParseDuration(since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", since, err)
		}
		req.Since = time.Now().Add(-d)
	}

	return commands.RunMonitor(cmd.Context(), req, os.Stdout)
}
