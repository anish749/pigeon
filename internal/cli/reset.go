package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var resetCmd = &cobra.Command{
	Use:     "reset",
	Short:   "Delete all synced data for a platform/account",
	GroupID: groupMaintenance,
	Long: `Deletes all synced message data and sync cursors for a workspace/account.
The next daemon start will re-sync from scratch.`,
	Example: `  pigeon reset --platform=slack --account=acme-corp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		platform, err := cmd.Flags().GetString("platform")
		if err != nil {
			return err
		}
		account, err := cmd.Flags().GetString("account")
		if err != nil {
			return err
		}
		return commands.RunReset(platform, account)
	},
}

var resetWhatsAppCmd = &cobra.Command{
	Use:    "reset-whatsapp",
	Short:  "Delete WhatsApp device pairing and data",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		account, err := cmd.Flags().GetString("account")
		if err != nil {
			return err
		}
		return commands.RunResetWhatsApp(account)
	},
}

func init() {
	resetCmd.Flags().StringP("platform", "p", "", "platform name")
	resetCmd.Flags().StringP("account", "a", "", "account/workspace name")
	resetCmd.MarkFlagRequired("platform")
	resetCmd.MarkFlagRequired("account")
	resetWhatsAppCmd.Flags().String("account", "", "WhatsApp account")
	rootCmd.AddCommand(resetCmd, resetWhatsAppCmd)
}
