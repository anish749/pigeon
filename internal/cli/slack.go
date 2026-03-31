package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var generateManifestCmd = &cobra.Command{
	Use:   "generate-manifest",
	Short: "Generate a Slack app manifest for a workspace",
	Long: `Renders the Slack app manifest template (manifests/slack-app.yaml) with
the given username and workspace name, prints it to stdout, and copies
it to the clipboard. Use this before creating or updating a Slack app.`,
	Example: `  pigeon generate-manifest --username=Anish --workspace=acme-corp`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return commands.RunGenerateManifest(
			mustString(cmd, "username"),
			mustString(cmd, "workspace"),
		)
	},
}

func init() {
	generateManifestCmd.Flags().String("username", "", "display name for the bot owner")
	generateManifestCmd.Flags().String("workspace", "", "Slack workspace name")
	generateManifestCmd.MarkFlagRequired("username")
	generateManifestCmd.MarkFlagRequired("workspace")
	rootCmd.AddCommand(generateManifestCmd)
}
