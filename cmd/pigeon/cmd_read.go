package main

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var readCmd = &cobra.Command{
	Use:   "read",
	Short: "Read messages from a conversation",
	Example: `  pigeon read --platform=whatsapp --account=+14155551234 --contact=Alice
  pigeon read --platform=slack --account=acme-corp --contact=#engineering --last=50
  pigeon read --platform=whatsapp --account=+14155551234 --contact=Bob --since=2h`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunRead(flagsToArgs(cmd, "platform", "account", "contact", "date", "last", "since"))
	},
}

func init() {
	readCmd.Flags().StringP("platform", "p", "", "platform name [required]")
	readCmd.Flags().StringP("account", "a", "", "account name [required]")
	readCmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel [required]")
	readCmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	readCmd.Flags().Int("last", 0, "show last N messages")
	readCmd.Flags().String("since", "", "messages from last duration (e.g. 30m, 2h, 7d)")
	rootCmd.AddCommand(readCmd)
}
