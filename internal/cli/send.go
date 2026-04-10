package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "send",
		Short:   "Send a message",
		GroupID: groupSending,
		Long: `Send a message through the daemon's connected clients.

Target flags are platform-specific:
  Slack:    --user-id (DMs) or --channel (channels, group DMs)
  WhatsApp: --contact (name or phone number)

By default, Slack messages are sent as the bot. Use --as-user to send as yourself.
Use --thread to reply to a thread, and --broadcast to also post the reply to the channel.
Run 'pigeon list' to find user IDs and channel names.`,
		Example: `  # Slack
  pigeon send -p slack -a acme-corp --user-id U07HF6KQ7PY -m "hey"
  pigeon send -p slack -a acme-corp --user-id U07HF6KQ7PY --as-user -m "sent as me"
  pigeon send -p slack -a acme-corp --channel '#engineering' -m "deploying now"
  pigeon send -p slack -a acme-corp --channel '#engineering' --thread 1711568938.123456 -m "fixed!"
  pigeon send -p slack -a acme-corp --channel '@mpdm-alice--bob-1' -m "hey all"

  # WhatsApp
  pigeon send -p whatsapp -a +14155551234 --contact Alice -m "hey, are you free?"`,
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
			userID, err := cmd.Flags().GetString("user-id")
			if err != nil {
				return err
			}
			channel, err := cmd.Flags().GetString("channel")
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
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}

			switch platform {
			case "slack":
				if contact != "" {
					return fmt.Errorf("use --user-id or --channel for Slack, not --contact")
				}
			case "whatsapp":
				if userID != "" || channel != "" {
					return fmt.Errorf("use --contact for WhatsApp, not --user-id or --channel")
				}
			}

			return commands.RunSend(commands.SendParams{
				Platform:  platform,
				Account:   account,
				UserID:    userID,
				Channel:   channel,
				Contact:   contact,
				Message:   message,
				Thread:    thread,
				Broadcast: broadcast,
				AsUser:    asUser,
				DryRun:    dryRun,
				Force:     force,
			})
		},
	}

	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.Flags().StringP("message", "m", "", "message text")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("message")

	// Target flags — mutually exclusive.
	cmd.Flags().String("user-id", "", "Slack user ID for DMs (U-prefixed, from 'pigeon list')")
	cmd.Flags().String("channel", "", "Slack channel (#name) or group DM (@mpdm-...)")
	cmd.Flags().StringP("contact", "c", "", "WhatsApp contact name or phone number")
	cmd.MarkFlagsMutuallyExclusive("user-id", "channel", "contact")
	cmd.MarkFlagsOneRequired("user-id", "channel", "contact")

	// Slack-specific flags.
	cmd.Flags().String("thread", "", "thread timestamp to reply to")
	cmd.Flags().Bool("broadcast", false, "broadcast thread reply to channel")
	cmd.Flags().Bool("as-user", false, "send as yourself instead of the bot (Slack only)")
	cmd.Flags().Bool("dry-run", false, "validate without sending")
	cmd.Flags().Bool("force", false, "send even if the thread is not found locally")

	return cmd
}
