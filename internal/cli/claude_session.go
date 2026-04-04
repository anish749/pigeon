package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

var claudeSessionCmd = &cobra.Command{
	Use:     "claude",
	Short:   "Start or resume a Claude Code session for a platform account",
	GroupID: groupDaemon,
	Long: `Create or resume a Claude Code session bound to a messaging platform account.

The session receives messages sent to the pigeon bot in that account
and can respond through pigeon. Session state is persisted so you can
resume where you left off.

If a session already exists for the platform+account, you'll be asked
whether to continue it or create a new one.`,
	Example: `  pigeon claude
  pigeon claude --platform slack --account acme-corp
  pigeon claude -p whatsapp -a +14155551234`,
	RunE: func(cmd *cobra.Command, args []string) error {
		platform, _ := cmd.Flags().GetString("platform")
		account, _ := cmd.Flags().GetString("account")
		return commands.RunClaudeSession(commands.ClaudeSessionParams{
			Platform: platform,
			Account:  account,
		})
	},
}

func init() {
	claudeSessionCmd.Flags().StringP("platform", "p", "", "platform name (slack, whatsapp)")
	claudeSessionCmd.Flags().StringP("account", "a", "", "account name (workspace or phone number)")
	rootCmd.AddCommand(claudeSessionCmd)
}
