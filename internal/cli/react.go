package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
)

func newReactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "react",
		Short:   "React to a message with an emoji",
		GroupID: groupSending,
		Long: `React to a message with an emoji through the daemon's connected clients.

For Slack, the emoji is a name without colons (e.g. thumbsup, tada).
For WhatsApp, the emoji is a Unicode character (e.g. 👍, 🎉).

Target flags are platform-specific:
  Slack:    --user-id (DMs) or --channel (channels, group DMs)
  WhatsApp: --contact (name or phone number)

Use --remove to remove a reaction.`,
		Example: `  # Slack
  pigeon react -p slack -a acme-corp --channel '#engineering' -m 1711568938.123456 -e thumbsup
  pigeon react -p slack -a acme-corp --user-id U07HF6KQ7PY -m 1711568938.123456 -e thumbsup
  pigeon react -p slack -a acme-corp --channel '#engineering' -m 1711568938.123456 -e thumbsup --remove

  # WhatsApp
  pigeon react -p whatsapp -a +14155551234 --contact Alice -m 3EB0A1B2C3D4E5F6 -e 👍`,
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
			messageID, err := cmd.Flags().GetString("message-id")
			if err != nil {
				return err
			}
			emoji, err := cmd.Flags().GetString("emoji")
			if err != nil {
				return err
			}
			remove, err := cmd.Flags().GetBool("remove")
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

			return commands.RunReact(commands.ReactParams{
				Platform:  platform,
				Account:   account,
				UserID:    userID,
				Channel:   channel,
				Contact:   contact,
				MessageID: messageID,
				Emoji:     emoji,
				Remove:    remove,
				Force:     force,
			})
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account name")
	// Target flags — mutually exclusive.
	cmd.Flags().String("user-id", "", "Slack user ID for DMs (U-prefixed, from 'pigeon list')")
	cmd.Flags().String("channel", "", "Slack channel (#name) or group DM (@mpdm-...)")
	cmd.Flags().StringP("contact", "c", "", "WhatsApp contact name or phone number")
	cmd.MarkFlagsMutuallyExclusive("user-id", "channel", "contact")
	cmd.MarkFlagsOneRequired("user-id", "channel", "contact")

	cmd.Flags().StringP("message-id", "m", "", "message ID (Slack timestamp or WhatsApp message key)")
	cmd.Flags().StringP("emoji", "e", "", "emoji (name for Slack, Unicode for WhatsApp)")
	cmd.Flags().Bool("remove", false, "remove the reaction instead of adding it")
	cmd.Flags().Bool("force", false, "skip local message validation")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("message-id")
	cmd.MarkFlagRequired("emoji")
	return cmd
}
