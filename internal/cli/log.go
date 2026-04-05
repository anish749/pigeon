package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/logging"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log",
		Short:   "Tail pigeon log files",
		GroupID: groupDaemon,
		Long:    `Tails the daemon and MCP log files, interleaving output in real time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := cmd.Flags().GetInt("lines")
			if err != nil {
				return err
			}
			follow, err := cmd.Flags().GetBool("follow")
			if err != nil {
				return err
			}
			return logging.Tail(n, follow)
		},
	}
	cmd.Flags().IntP("lines", "n", 50, "last N lines to show from each file")
	cmd.Flags().BoolP("follow", "f", false, "follow log output in real time")
	return cmd
}
