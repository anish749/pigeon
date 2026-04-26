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

func newSetupLinearCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "setup-linear",
		Short:   "Register a Linear workspace for issue tracking",
		GroupID: groupSetup,
		Long:    `Registers the workspace from the linear CLI. Run 'linear auth login' first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunSetupLinear(args)
		},
	}
}

func newSetupJiraCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "setup-jira [path-to-jira-cli-config]",
		Short:   "Register a jira-cli configuration for Jira issue tracking",
		GroupID: groupSetup,
		Long: `Binds a jira-cli configuration to pigeon's ingest.

Pigeon reads the bound jira-cli YAML for server / login / project / auth at
runtime — no Jira credentials are stored in pigeon's own config.

Pre-requisites:
  1. Install jira-cli (https://github.com/ankitpokhrel/jira-cli)
  2. Generate an Atlassian API token at
     https://id.atlassian.com/manage-profile/security/api-tokens
  3. Export it: export JIRA_API_TOKEN=<token>
  4. Run jira init to create the jira-cli YAML

Then:
  pigeon setup-jira              # use $JIRA_CONFIG_FILE or jira-cli's default path
  pigeon setup-jira /path/to/jira.yml

The command verifies auth end-to-end via /myself before saving — a 401
here is the same diagnostic as 'jira me' returning 401, but caught at
setup time rather than from a daemon log later.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return commands.RunSetupJira(args)
		},
	}
}
