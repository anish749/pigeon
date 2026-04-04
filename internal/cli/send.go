package cli

import (
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/commands"
)

func newSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "send",
		Short:   "Send a message",
		GroupID: groupSending,
		Long: `Send a message through the daemon's connected clients.

By default, Slack messages are sent as the bot. Use --as-user to send as yourself.
Use --thread to reply to a thread, and --broadcast to also post the reply to the channel.

If your Slack app was installed before bot sending was added, re-run 'pigeon setup-slack'
to update scopes.`,
		Example: `  pigeon send -p whatsapp -a +14155551234 -c Alice -m "hey, are you free?"
  pigeon send -p slack -a acme-corp -c #engineering -m "deploying now"
  pigeon send -p slack -a acme-corp -c @alice -m "quick question"
  pigeon send -p slack -a acme-corp -c #engineering --thread 1711568938.123456 -m "fixed!"
  pigeon send -p slack -a acme-corp -c #engineering --thread 1711568938.123456 --broadcast -m "resolved"
  pigeon send -p slack -a acme-corp -c @alice --as-user -m "sent as me, not the bot"`,
		PreRunE: ensureDaemon,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := cmd.Flags().GetString("platform")
			if err != nil {
				return err
			}
			account, err := cmd.Flags().GetString("account")
			if err != nil {
				return err
			}
			contact, err := cmd.Flags().GetString("contact")
			if err != nil {
				return err
			}
			message, err := cmd.Flags().GetString("message")
			if err != nil {
				return err
			}
			thread, err := cmd.Flags().GetString("thread")
			if err != nil {
				return err
			}
			broadcast, err := cmd.Flags().GetBool("broadcast")
			if err != nil {
				return err
			}
			asUser, err := cmd.Flags().GetBool("as-user")
			if err != nil {
				return err
			}
			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}
			return commands.RunSend(commands.SendParams{
				Platform:  platform,
				Account:   account,
				Contact:   contact,
				Message:   message,
				Thread:    thread,
				Broadcast: broadcast,
				AsUser:    asUser,
				DryRun:    dryRun,
			})
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel")
	cmd.Flags().StringP("message", "m", "", "message text")
	cmd.Flags().String("thread", "", "thread timestamp to reply to")
	cmd.Flags().Bool("broadcast", false, "broadcast thread reply to channel")
	cmd.Flags().Bool("as-user", false, "send as yourself instead of the bot")
	cmd.Flags().Bool("dry-run", false, "resolve contact and validate without sending")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("contact")
	cmd.MarkFlagRequired("message")
	return cmd
}
