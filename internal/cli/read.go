package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var readCmd = &cobra.Command{
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

func init() {
	readCmd.Flags().StringP("platform", "p", "", "platform name")
	readCmd.Flags().StringP("account", "a", "", "account name")
	readCmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel")
	readCmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	readCmd.Flags().Int("last", 0, "show last N messages")
	readCmd.Flags().String("since", "", "messages from last duration (e.g. 30m, 2h, 7d)")
	readCmd.MarkFlagRequired("platform")
	readCmd.MarkFlagRequired("account")
	readCmd.MarkFlagRequired("contact")
	rootCmd.AddCommand(readCmd)
}

