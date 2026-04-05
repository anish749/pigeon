package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/logging"
)

func newLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "log [-- tail-flags...]",
		Short:   "Tail pigeon log files (extra args forwarded to tail)",
		GroupID: groupDaemon,
		Long:    `Tails the daemon and MCP log files with tail -f. Extra arguments after -- are forwarded directly to tail (e.g. -n 100).`,
		Example: "  pigeon log\n  pigeon log -- -n 100",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return logging.Tail(args)
		},
	}
}
