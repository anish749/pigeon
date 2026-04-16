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

Pick a platform subcommand:
  pigeon react slack    — reactions in Slack channels, DMs, group DMs
  pigeon react whatsapp — reactions on WhatsApp messages

Use --remove to remove a reaction.`,
	}

	// Shared flags live on the parent so they apply to every subcommand.
	cmd.PersistentFlags().StringP("account", "a", "", "account name")
	cmd.PersistentFlags().StringP("message-id", "m", "", "message ID (Slack timestamp or WhatsApp message key)")
	cmd.PersistentFlags().StringP("emoji", "e", "", "emoji (name for Slack, Unicode for WhatsApp)")
	cmd.PersistentFlags().Bool("remove", false, "remove the reaction instead of adding it")
	if err := cmd.MarkPersistentFlagRequired("account"); err != nil {
		panic(err)
	}
	if err := cmd.MarkPersistentFlagRequired("message-id"); err != nil {
		panic(err)
	}
	if err := cmd.MarkPersistentFlagRequired("emoji"); err != nil {
		panic(err)
	}

	cmd.AddCommand(newReactSlackCmd(), newReactWhatsappCmd())
	return cmd
}

func newReactSlackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "React to a Slack message",
		Long: `React to a Slack message with an emoji. The emoji is a name without colons
(e.g. thumbsup, tada). Use --remove to remove a reaction.`,
		Example: `  pigeon react slack -a acme-corp -c '#engineering' -m 1711568938.123456 -e thumbsup
  pigeon react slack -a acme-corp --user-id U07HF6KQ7PY -m 1711568938.123456 -e thumbsup
  pigeon react slack -a acme-corp -c '#engineering' -m 1711568938.123456 -e thumbsup --remove`,
		PreRunE: ensureDaemon,
		RunE:    runReactSlack,
	}
	cmd.Flags().String("user-id", "", "Slack user ID for DMs (U-prefixed, from 'pigeon list')")
	cmd.Flags().StringP("channel", "c", "", "Slack channel (#name) or group DM (@mpdm-...)")
	cmd.MarkFlagsMutuallyExclusive("user-id", "channel")
	cmd.MarkFlagsOneRequired("user-id", "channel")
	return cmd
}

func newReactWhatsappCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whatsapp",
		Short: "React to a WhatsApp message",
		Long: `React to a WhatsApp message with a Unicode emoji (e.g. 👍, 🎉).
Use --remove to remove a reaction.`,
		Example: `  pigeon react whatsapp -a +14155551234 -c Alice -m 3EB0A1B2C3D4E5F6 -e 👍`,
		PreRunE: ensureDaemon,
		RunE:    runReactWhatsapp,
	}
	cmd.Flags().StringP("contact", "c", "", "contact name or phone number")
	if err := cmd.MarkFlagRequired("contact"); err != nil {
		panic(err)
	}
	return cmd
}

func runReactSlack(cmd *cobra.Command, args []string) error {
	account, err := cmd.Flags().GetString("account")
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
	userID, err := cmd.Flags().GetString("user-id")
	if err != nil {
		return err
	}
	channel, err := cmd.Flags().GetString("channel")
	if err != nil {
		return err
	}
	return commands.RunReact(commands.ReactParams{
		Platform:  "slack",
		Account:   account,
		UserID:    userID,
		Channel:   channel,
		MessageID: messageID,
		Emoji:     emoji,
		Remove:    remove,
	})
}

func runReactWhatsapp(cmd *cobra.Command, args []string) error {
	account, err := cmd.Flags().GetString("account")
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
	contact, err := cmd.Flags().GetString("contact")
	if err != nil {
		return err
	}
	return commands.RunReact(commands.ReactParams{
		Platform:  "whatsapp",
		Account:   account,
		Contact:   contact,
		MessageID: messageID,
		Emoji:     emoji,
		Remove:    remove,
	})
}
