package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete",
		Short:   "Delete a sent message",
		GroupID: groupSending,
		Long: `Delete a message the bot previously sent through the daemon's connected clients.

Pick a platform subcommand:
  pigeon delete slack — delete a bot message in Slack

Only messages sent by the bot can be deleted.`,
	}

	cmd.PersistentFlags().StringP("account", "a", "", "account name")
	cmd.PersistentFlags().StringP("message-id", "m", "", "message timestamp to delete")
	if err := cmd.MarkPersistentFlagRequired("account"); err != nil {
		panic(err)
	}
	if err := cmd.MarkPersistentFlagRequired("message-id"); err != nil {
		panic(err)
	}

	cmd.AddCommand(newDeleteSlackCmd())
	return cmd
}

func newDeleteSlackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "Delete a Slack message sent by the bot",
		Long: `Delete a message the bot previously sent in a Slack channel or DM.
Only messages sent by the bot can be deleted.`,
		Example: `  pigeon delete slack -a acme-corp -c '#engineering' -m 1711568938.123456
  pigeon delete slack -a acme-corp --user-id U07HF6KQ7PY -m 1711568938.123456`,
		PreRunE: ensureDaemon,
		RunE:    runDeleteSlack,
	}
	cmd.Flags().String("user-id", "", "Slack user ID for DMs (U-prefixed, from 'pigeon list')")
	cmd.Flags().StringP("channel", "c", "", "Slack channel (#name) or group DM (@mpdm-...)")
	cmd.MarkFlagsMutuallyExclusive("user-id", "channel")
	cmd.MarkFlagsOneRequired("user-id", "channel")
	return cmd
}

func runDeleteSlack(cmd *cobra.Command, args []string) error {
	account, err := cmd.Flags().GetString("account")
	if err != nil {
		return err
	}
	messageID, err := cmd.Flags().GetString("message-id")
	if err != nil {
		return err
	}
	userID, err := cmd.Flags().GetString("user-id")
	if err != nil {
		return err
	}
	channel, err := cmd.Flags().GetString("channel")
	if err != nil {
		return err
	}
	return commands.RunDelete(commands.DeleteParams{
		Platform:  "slack",
		Account:   account,
		UserID:    userID,
		Channel:   channel,
		MessageID: messageID,
	})
}
