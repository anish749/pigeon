package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var sendCmd = &cobra.Command{
	Use:     "send",
	Short:   "Send a message",
	GroupID: groupSending,
	Long: `Send a message through the daemon's connected clients.

Slack sending requires chat:write scope. If your Slack app was installed
before this feature, re-run 'pigeon setup-slack' to update scopes.`,
	Example: `  pigeon send --platform=whatsapp --account=+14155551234 --contact=Alice -m "hey, are you free?"
  pigeon send --platform=slack --account=acme-corp --contact=#engineering -m "deploying now"`,
	PreRunE: ensureDaemon,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunSend(
			mustString(cmd, "platform"),
			mustString(cmd, "account"),
			mustString(cmd, "contact"),
			mustString(cmd, "message"),
		)
	},
}

func init() {
	sendCmd.Flags().StringP("platform", "p", "", "platform name")
	sendCmd.Flags().StringP("account", "a", "", "account name")
	sendCmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel")
	sendCmd.Flags().StringP("message", "m", "", "message text")
	sendCmd.MarkFlagRequired("platform")
	sendCmd.MarkFlagRequired("account")
	sendCmd.MarkFlagRequired("contact")
	sendCmd.MarkFlagRequired("message")
	rootCmd.AddCommand(sendCmd)
}
