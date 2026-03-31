package main

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List platforms, accounts, or conversations",
	Example: `  pigeon list
  pigeon list --platform=whatsapp
  pigeon list --platform=whatsapp --account=+14155551234`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunList(flagsToArgs(cmd, "platform", "account"))
	},
}

func init() {
	listCmd.Flags().StringP("platform", "p", "", "filter by platform (e.g. whatsapp, slack)")
	listCmd.Flags().StringP("account", "a", "", "filter by account (e.g. +14155551234, acme-corp)")
	rootCmd.AddCommand(listCmd)
}
