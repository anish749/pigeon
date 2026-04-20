package cli

import (
	"github.com/spf13/cobra"

	a "github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/commands"
)

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "read",
		Short:   "Read messages from a conversation",
		GroupID: groupReading,
		Example: `  pigeon read --platform=whatsapp --account=+14155551234 --contact=Alice
  pigeon read --platform=slack --account=acme-corp --contact=#engineering --last=50
  pigeon read --platform=whatsapp --account=+14155551234 --contact=Bob --since=2h`,
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
			contact, err := cmd.Flags().GetString("contact")
			if err != nil {
				return err
			}
			date, err := cmd.Flags().GetString("date")
			if err != nil {
				return err
			}
			last, err := cmd.Flags().GetInt("last")
			if err != nil {
				return err
			}
			since, err := cmd.Flags().GetString("since")
			if err != nil {
				return err
			}
			if err := validateAccountInWorkspace(a.New(platform, account)); err != nil {
				return err
			}
			return commands.RunRead(commands.ReadParams{
				Platform: platform,
				Account:  account,
				Contact:  contact,
				Date:     date,
				Last:     last,
				Since:    since,
			})
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel")
	cmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	cmd.Flags().Int("last", 0, "show last N messages (default 25 when no filter specified)")
	cmd.Flags().String("since", "", "messages from last duration (e.g. 30m, 2h, 7d)")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("contact")
	return cmd
}
