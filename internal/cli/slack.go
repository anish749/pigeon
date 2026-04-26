package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newGenerateManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate-manifest",
		Short:   "Generate a Slack app manifest for a workspace",
		GroupID: groupSlack,
		Long: `Renders the Slack app manifest template (manifests/slack-app.yaml) with
the given username and workspace name, prints it to stdout, and copies
it to the clipboard. Use this before creating or updating a Slack app.`,
		Example: `  pigeon generate-manifest --username=Anish --slack-workspace=acme-corp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			username, err := cmd.Flags().GetString("username")
			if err != nil {
				return err
			}
			workspace, err := cmd.Flags().GetString("slack-workspace")
			if err != nil {
				return err
			}
			return commands.RunGenerateManifest(username, workspace)
		},
	}
	cmd.Flags().String("username", "", "display name for the bot owner")
	cmd.Flags().String("slack-workspace", "", "Slack workspace display name baked into the manifest (interactive picker if omitted)")
	cmd.MarkFlagRequired("username")
	return cmd
}
