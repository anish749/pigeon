package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

func newGenerateManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate-manifest",
		Short:   "Generate a Slack app manifest for a workspace",
		GroupID: groupSlack,
		Long: `Renders the Slack app manifest template (manifests/slack-app.yaml) with
the given username and workspace name, prints it to stdout, and copies
it to the clipboard. Use this before creating or updating a Slack app.`,
		Example: `  pigeon generate-manifest --username=Anish --workspace=acme-corp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			username, err := cmd.Flags().GetString("username")
			if err != nil {
				return err
			}
			workspace, err := cmd.Flags().GetString("workspace")
			if err != nil {
				return err
			}
			return commands.RunGenerateManifest(username, workspace)
		},
	}
	cmd.Flags().String("username", "", "display name for the bot owner")
	cmd.Flags().String("workspace", "", "Slack workspace name")
	cmd.MarkFlagRequired("username")
	cmd.MarkFlagRequired("workspace")
	return cmd
}
