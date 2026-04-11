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
	cmd.Flags().String("workspace", "", "Slack workspace name (interactive picker if omitted)")
	cmd.MarkFlagRequired("username")
	return cmd
}

func newAutoApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auto-approve",
		Short:   "Manage DM users that bypass outbox review",
		GroupID: groupSlack,
		Long: `Manage the per-workspace list of Slack user IDs whose DMs bypass
outbox review. Messages to these users are sent immediately, even
within a Claude session.

Use 'pigeon list' to find Slack user IDs.`,
	}

	addCmd := &cobra.Command{
		Use:     "add",
		Short:   "Add a user to the auto-approve list",
		Example: `  pigeon auto-approve add --workspace=acme-corp --user-id=U07HF6KQ7PY`,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, _ := cmd.Flags().GetString("workspace")
			userID, _ := cmd.Flags().GetString("user-id")
			return commands.RunAutoApproveAdd(workspace, userID)
		},
	}
	addCmd.Flags().String("workspace", "", "Slack workspace name")
	addCmd.Flags().String("user-id", "", "Slack user ID (U-prefixed)")
	addCmd.MarkFlagRequired("workspace")
	addCmd.MarkFlagRequired("user-id")

	removeCmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove a user from the auto-approve list",
		Example: `  pigeon auto-approve remove --workspace=acme-corp --user-id=U07HF6KQ7PY`,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, _ := cmd.Flags().GetString("workspace")
			userID, _ := cmd.Flags().GetString("user-id")
			return commands.RunAutoApproveRemove(workspace, userID)
		},
	}
	removeCmd.Flags().String("workspace", "", "Slack workspace name")
	removeCmd.Flags().String("user-id", "", "Slack user ID (U-prefixed)")
	removeCmd.MarkFlagRequired("workspace")
	removeCmd.MarkFlagRequired("user-id")

	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "List auto-approved users for a workspace",
		Example: `  pigeon auto-approve list --workspace=acme-corp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, _ := cmd.Flags().GetString("workspace")
			return commands.RunAutoApproveList(workspace)
		},
	}
	listCmd.Flags().String("workspace", "", "Slack workspace name")
	listCmd.MarkFlagRequired("workspace")

	cmd.AddCommand(addCmd, removeCmd, listCmd)
	return cmd
}
