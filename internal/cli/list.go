package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List platforms, accounts, or conversations",
		GroupID: groupReading,
		Example: `  pigeon list
  pigeon list --platform=slack
  pigeon list --platform=slack --account=acme-corp
  pigeon list --platform=whatsapp --account=+14155551234
  pigeon list --since=2h
  pigeon list --platform=slack --since=7d`,
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
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return err
			}
			return commands.RunList(commands.ListParams{
				Platform: platform,
				Account:  account,
				Since:    since,
			})
		},
	}
	cmd.Flags().StringP("platform", "p", "", "filter by platform (e.g. whatsapp, slack)")
	cmd.Flags().StringP("account", "a", "", "filter by account (e.g. +14155551234, acme-corp)")
	cmd.Flags().String("since", "", "only show conversations with recent activity (e.g. 2h, 7d)")
	return cmd
}
