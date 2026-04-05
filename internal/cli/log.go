package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/logging"
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
			return logging.Tail(n)
		},
	}
	cmd.Flags().IntP("lines", "n", 50, "last N lines to show from each file before following")
	return cmd
}
