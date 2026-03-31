package main

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete all synced data for a platform/account",
	Long: `Deletes all synced message data and sync cursors for a workspace/account.
The next daemon start will re-sync from scratch.`,
	Example: `  pigeon reset --platform=slack --account=acme-corp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunReset(flagsToArgs(cmd, "platform", "account"))
	},
}

var resetWhatsAppCmd = &cobra.Command{
	Use:    "reset-whatsapp",
	Short:  "Delete WhatsApp device pairing and data",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunResetWhatsApp(flagsToArgs(cmd, "account"))
	},
}

func init() {
	resetCmd.Flags().StringP("platform", "p", "", "platform name [required]")
	resetCmd.Flags().StringP("account", "a", "", "account/workspace name [required]")
	resetWhatsAppCmd.Flags().String("account", "", "WhatsApp account [required]")
	rootCmd.AddCommand(resetCmd, resetWhatsAppCmd)
}
