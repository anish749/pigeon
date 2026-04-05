package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/tui"
)

func newReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "review",
		Short:   "Review pending outbox items in a TUI",
		GroupID: groupSending,
		Long: `Open the outbox review panel — a terminal UI for reviewing and approving
outgoing messages before they are sent.

When Claude sends a message through a pigeon session, the message is held in
the outbox for review instead of being sent immediately. Use this command to
approve, reject, or provide feedback on pending messages.`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.RunReview()
		},
	}
}
