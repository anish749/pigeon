package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
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
	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account/workspace name")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	return cmd
}

func newResetWhatsAppCmd() *cobra.Command {
	cmd := &cobra.Command{
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
	cmd.Flags().String("account", "", "WhatsApp account")
	return cmd
}
