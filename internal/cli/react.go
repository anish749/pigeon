package cli

import (
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

Use --remove to remove a reaction.`,
		Example: `  pigeon react -p slack -a acme-corp -c #engineering -m 1711568938.123456 -e thumbsup
  pigeon react -p whatsapp -a +14155551234 -c Alice -m 3EB0A1B2C3D4E5F6 -e 👍
  pigeon react -p slack -a acme-corp -c #engineering -m 1711568938.123456 -e thumbsup --remove`,
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
			return commands.RunReact(commands.ReactParams{
				Platform:  platform,
				Account:   account,
				Contact:   contact,
				MessageID: messageID,
				Emoji:     emoji,
				Remove:    remove,
			})
		},
	}
	cmd.Flags().StringP("platform", "p", "", "platform name")
	cmd.Flags().StringP("account", "a", "", "account name")
	cmd.Flags().StringP("contact", "c", "", "contact name, phone, or channel")
	cmd.Flags().StringP("message-id", "m", "", "message ID (Slack timestamp or WhatsApp message key)")
	cmd.Flags().StringP("emoji", "e", "", "emoji (name for Slack, Unicode for WhatsApp)")
	cmd.Flags().Bool("remove", false, "remove the reaction instead of adding it")
	cmd.MarkFlagRequired("platform")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("contact")
	cmd.MarkFlagRequired("message-id")
	cmd.MarkFlagRequired("emoji")
	return cmd
}
