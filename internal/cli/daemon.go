package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newDaemonCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "daemon",
		Short:   "Manage the background daemon",
		GroupID: groupDaemon,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the daemon in the background",
			RunE: func(cmd *cobra.Command, args []string) error {
				return commands.DaemonStart()
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the daemon",
			RunE: func(cmd *cobra.Command, args []string) error {
				return commands.DaemonStop()
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Check if the daemon is running",
			RunE: func(cmd *cobra.Command, args []string) error {
				return commands.DaemonStatus()
			},
		},
		&cobra.Command{
			Use:   "restart",
			Short: "Restart the daemon",
			RunE: func(cmd *cobra.Command, args []string) error {
				return commands.DaemonRestart()
			},
		},
		&cobra.Command{
			Use:    "_run",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				return commands.DaemonRun(version)
			},
		},
	)
	return cmd
}
