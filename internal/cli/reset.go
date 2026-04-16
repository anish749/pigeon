package cli

import (
	"fmt"

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

Use 'pigeon unlink' to fully remove an account and its config.`,
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

func newUnlinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Remove an account and delete its data",
		Long: `Removes an account from pigeon's config and deletes all synced data.
This is the inverse of the setup commands. For WhatsApp, also unpairs the
device from your phone.

Supported platforms: slack, whatsapp`,
		Example: `  pigeon unlink --platform=slack --account=acme-corp
  pigeon unlink --platform=whatsapp --account=+14155551234`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			switch platform {
			case "slack":
				return commands.RunUnlinkSlack(account)
			case "whatsapp":
				return commands.RunUnlinkWhatsApp(account)
			default:
				return fmt.Errorf("unlink not supported for platform %q (supported: slack, whatsapp)", platform)
			}
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform (slack, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "account/workspace name")
	cmd.MarkFlagRequired("platform")
	return cmd
}
