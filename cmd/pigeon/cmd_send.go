package main

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send a message",
	Long: `Send a message through the daemon's connected clients.

Slack sending requires chat:write scope. If your Slack app was installed
before this feature, re-run 'pigeon setup-slack' to update scopes.`,
	Example: `  pigeon send --platform=whatsapp --account=+14155551234 --contact=Alice -m "hey, are you free?"
  pigeon send --platform=slack --account=acme-corp --contact=#engineering -m "deploying now"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunSend(flagsToArgs(cmd, "platform", "account", "contact", "m"))
	},
}

func init() {
	sendCmd.Flags().StringP("platform", "p", "", "platform name [required]")
	sendCmd.Flags().StringP("account", "a", "", "account name [required]")
	sendCmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel [required]")
	sendCmd.Flags().StringP("m", "m", "", "message text [required]")
	rootCmd.AddCommand(sendCmd)
}
