package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List platforms, accounts, or conversations",
		GroupID: groupReading,
		Example: `  pigeon list
  pigeon list --platform=whatsapp
  pigeon list --platform=whatsapp --account=+14155551234`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			return commands.RunList(platform, account)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (e.g. whatsapp, slack)")
	cmd.Flags().StringP("account", "a", "", "filter by account (e.g. +14155551234, acme-corp)")
	return cmd
}
