package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reset",
		Short:   "Delete synced message data for a platform/account",
		GroupID: groupMaintenance,
		Long: `Deletes synced message files and sync cursors for a workspace/account.
Device pairings, auth tokens, and config are preserved.
The next daemon start will re-sync messages from scratch.

Use unlink-whatsapp to fully unpair a WhatsApp device.`,
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

func newUnlinkWhatsAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink-whatsapp",
		Short: "Unpair WhatsApp device, delete data, and remove config",
		Long: `Unlinks the WhatsApp device from your phone, deletes all synced
message data, and removes the account from pigeon's config.
This is the inverse of setup-whatsapp. You will need to re-pair
with a QR code to use this account again.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			return commands.RunUnlinkWhatsApp(account)
		},
	}
	cmd.Flags().String("account", "", "WhatsApp account")
	return cmd
}
