package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/config"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Short:   "Manage workspaces",
		GroupID: groupSetup,
		Long: `List, create, and manage workspaces.

A workspace groups accounts across platforms (Slack, GWS, WhatsApp) so that
reads, searches, and identity resolution are scoped to those accounts.

Running 'pigeon workspace' with no subcommand lists all workspaces.`,
		Example: `  pigeon workspace
  pigeon workspace add work -p slack -a acme-corp
  pigeon workspace add work -p gws -a you@company.com
  pigeon workspace remove work -p slack -a acme-corp
  pigeon workspace delete work
  pigeon workspace default work`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return commands.RunWorkspaceList(cfg)
		},
	}

	cmd.AddCommand(
		newWorkspaceAddCmd(),
		newWorkspaceRemoveCmd(),
		newWorkspaceDeleteCmd(),
		newWorkspaceDefaultCmd(),
	)
	return cmd
}

func newWorkspaceAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <workspace>",
		Short: "Add an account to a workspace",
		Long: `Add a configured account to a workspace, creating the workspace if needed.
The account must already be set up (via setup-slack, setup-gws, etc.).`,
		Example: `  pigeon workspace add work -p slack -a acme-corp
  pigeon workspace add personal -p whatsapp -a +14155551234`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return commands.RunWorkspaceAdd(cfg, args[0], platform, account)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform (slack, gws, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	return cmd
}

func newWorkspaceRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <workspace>",
		Short:   "Remove an account from a workspace",
		Long:    `Remove an account from a workspace. If the workspace becomes empty, it is deleted.`,
		Example: `  pigeon workspace remove work -p slack -a acme-corp`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return commands.RunWorkspaceRemove(cfg, args[0], platform, account)
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform (slack, gws, whatsapp)")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	return cmd
}

func newWorkspaceDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <workspace>",
		Short:   "Delete a workspace",
		Example: `  pigeon workspace delete old-project`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			return commands.RunWorkspaceDelete(cfg, args[0])
		},
	}
}

func newWorkspaceDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default [workspace]",
		Short: "Show or set the default workspace",
		Long: `With no argument, prints the current default workspace.
With an argument, sets the default workspace.`,
		Example: `  pigeon workspace default
  pigeon workspace default work`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return commands.RunWorkspaceDefault(cfg, name)
		},
	}
}
