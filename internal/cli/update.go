package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/selfupdate"
)

func newUpdateCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "update",
		Short:   "Update pigeon to the latest version",
		GroupID: groupMaintenance,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return selfupdate.Update(version)
		},
	}
}
