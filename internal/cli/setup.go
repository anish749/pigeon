package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newSetupWhatsAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "setup-whatsapp",
		Short:   "Pair a WhatsApp device via QR code, save to config",
		GroupID: groupSetup,
		Example: `  pigeon setup-whatsapp
  pigeon setup-whatsapp --db=/path/to/whatsapp.db`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := cmd.Flags().GetString("db")
			if err != nil {
				return err
			}
			return commands.RunSetupWhatsApp(db)
		},
	}
	cmd.Flags().String("db", "", "SQLite database path (default: <data-dir>/whatsapp.db)")
	return cmd
}

func newSetupSlackCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "setup-slack",
		Short:   "Install a Slack app in a workspace via OAuth",
		GroupID: groupSetup,
		Long: `Installs the Slack app in a workspace via OAuth. Opens your browser
to Slack's authorization page — pick a workspace and approve.

To create a Slack app:
  1. Run: pigeon generate-manifest --username=You --workspace=acme-corp
  2. Go to https://api.slack.com/apps → "Create New App" → "From a manifest"
  3. Paste the manifest from your clipboard
  4. Under "Basic Information", copy client ID and client secret
  5. Under "Socket Mode", enable it and create an app-level token (xapp-...)
  6. pigeon setup-slack
  7. Your browser opens — pick a workspace and approve
  8. Done! Add more workspaces by running: pigeon setup-slack`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunSetupSlack(args)
		},
	}
}

func newSetupGWSCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "setup-gws",
		Short:   "Register a Google Workspace account (Gmail, Calendar, Drive)",
		GroupID: groupSetup,
		Long:    `Registers the account from the gws CLI. Run 'gws auth login' first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunSetupGWS(args)
		},
	}
}
