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
		last, _ := cmd.Flags().GetInt("last")
		return commands.RunRead(commands.ReadParams{
			Platform: mustString(cmd, "platform"),
			Account:  mustString(cmd, "account"),
			Contact:  mustString(cmd, "contact"),
			Date:     mustString(cmd, "date"),
			Last:     last,
			Since:    mustString(cmd, "since"),
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

func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
