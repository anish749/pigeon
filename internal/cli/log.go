package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/paths"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log",
		Short:   "Tail pigeon log files",
		GroupID: groupDaemon,
		Long:    `Tails the daemon and MCP log files, interleaving output in real time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			n, _ := cmd.Flags().GetInt("lines")

			var files []string
			for _, p := range []string{paths.DaemonLogPath(), paths.MCPLogPath()} {
				if _, err := os.Stat(p); err == nil {
					files = append(files, p)
				}
			}
			if len(files) == 0 {
				return fmt.Errorf("no log files found in %s", paths.StateDir())
			}

			tailArgs := append([]string{"-f", "-n", fmt.Sprintf("%d", n)}, files...)
			tail := exec.Command("tail", tailArgs...)
			tail.Stdout = os.Stdout
			tail.Stderr = os.Stderr
			return tail.Run()
		},
	}
	cmd.Flags().IntP("lines", "n", 50, "number of lines to show from each file before following")
	return cmd
}
