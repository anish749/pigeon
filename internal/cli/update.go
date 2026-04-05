package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/selfupdate"
)

func newUpdateCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "update",
		Short:   "Update pigeon to the latest version",
		GroupID: groupMaintenance,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			updated, err := selfupdate.Update(version)
			if err != nil {
				return err
			}
			if updated && daemon.IsRunning() {
				fmt.Fprintln(os.Stderr, "The daemon is still running the old version. It will automatically detect this update and restart itself.")
			}
			return nil
		},
	}
}
