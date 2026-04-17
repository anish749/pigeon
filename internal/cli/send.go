package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/commands"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

func newSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "send",
		Short:   "Send a message",
		GroupID: groupSending,
		Long: `Send a message through the daemon's connected clients.

Pick a platform subcommand:
  pigeon send slack    — Slack DMs, channels, group DMs, threads
  pigeon send whatsapp — WhatsApp contacts`,
	}

	// Shared flags live on the parent so they apply to every subcommand.
	cmd.PersistentFlags().StringP("account", "a", "", "account name")
	cmd.PersistentFlags().StringP("message", "m", "", "message text")
	cmd.PersistentFlags().Bool("dry-run", false, "validate without sending")
	if err := cmd.MarkPersistentFlagRequired("account"); err != nil {
		panic(err)
	}
	if err := cmd.MarkPersistentFlagRequired("message"); err != nil {
		panic(err)
	}

	cmd.AddCommand(newSendSlackCmd(), newSendWhatsappCmd())
	return cmd
}

func newSendSlackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slack",
		Short: "Send a Slack message",
		Long: `Send a Slack DM, channel message, group DM, or thread reply.

Standard Markdown is auto-converted to Slack mrkdwn (**bold** → *bold*,
*italic* → _italic_, ~~strike~~ → ~strike~, [text](url) → <url|text>).
You can also write mrkdwn directly: *bold*, _italic_, ~strike~, ` + "`code`" + `.

By default, messages are sent as the bot. Use --via pigeon-as-user to send as
the account owner who connected pigeon (uses their user token).
Use --thread to reply to a thread, and --broadcast to also post the reply to
the channel. Run 'pigeon list' to find user IDs and channel names.`,
		Example: `  pigeon send slack -a acme-corp --user-id U07HF6KQ7PY -m "hey"
  pigeon send slack -a acme-corp --user-id U07HF6KQ7PY --via pigeon-as-user -m "sent as me"
  pigeon send slack -a acme-corp -c '#engineering' -m "deploying now"
  pigeon send slack -a acme-corp -c '#engineering' --thread 1711568938.123456 -m "fixed!"
  pigeon send slack -a acme-corp -c '#engineering' --post-at 2026-04-11T09:00:00 -m "scheduled"
  pigeon send slack -a acme-corp -c '@mpdm-alice--bob-1' -m "hey all"`,
		PreRunE: ensureDaemon,
		RunE:    runSendSlack,
	}

	cmd.Flags().String("user-id", "", "Slack user ID for DMs (U-prefixed, from 'pigeon list')")
	cmd.Flags().StringP("channel", "c", "", "Slack channel (#name) or group DM (@mpdm-...)")
	cmd.MarkFlagsMutuallyExclusive("user-id", "channel")
	cmd.MarkFlagsOneRequired("user-id", "channel")

	cmd.Flags().String("thread", "", "thread timestamp to reply to")
	cmd.Flags().Bool("broadcast", false, "broadcast thread reply to channel")
	cmd.Flags().String("post-at", "", "when to send: ISO 8601 (2026-04-11T09:00:00, local timezone) or Unix timestamp (up to 120 days)")
	cmd.Flags().String("via", string(modelv1.ViaPigeonAsBot), "send identity: pigeon-as-bot (default) or pigeon-as-user (send as the account owner)")
	cmd.Flags().Bool("force", false, "send even if the thread is not found locally")
	return cmd
}

func newSendWhatsappCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "whatsapp",
		Short:   "Send a WhatsApp message",
		Long:    `Send a WhatsApp message to a contact by name or phone number.`,
		Example: `  pigeon send whatsapp -a +14155551234 -c Alice -m "hey, are you free?"`,
		PreRunE: ensureDaemon,
		RunE:    runSendWhatsapp,
	}

	cmd.Flags().StringP("contact", "c", "", "contact name or phone number")
	if err := cmd.MarkFlagRequired("contact"); err != nil {
		panic(err)
	}
	return cmd
}

func runSendSlack(cmd *cobra.Command, args []string) error {
	account, err := cmd.Flags().GetString("account")
	if err != nil {
		return err
	}
	message, err := cmd.Flags().GetString("message")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
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
	thread, err := cmd.Flags().GetString("thread")
	if err != nil {
		return err
	}
	broadcast, err := cmd.Flags().GetBool("broadcast")
	if err != nil {
		return err
	}
	postAt, err := cmd.Flags().GetString("post-at")
	if err != nil {
		return err
	}
	viaStr, err := cmd.Flags().GetString("via")
	if err != nil {
		return err
	}
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}

	if postAt != "" {
		ts, err := parsePostAt(postAt)
		if err != nil {
			return err
		}
		postAt = strconv.FormatInt(ts, 10)
	}

	return commands.RunSend(commands.SendParams{
		Platform:  "slack",
		Account:   account,
		UserID:    userID,
		Channel:   channel,
		Message:   message,
		Thread:    thread,
		Broadcast: broadcast,
		PostAt:    postAt,
		Via:       modelv1.Via(viaStr),
		DryRun:    dryRun,
		Force:     force,
	})
}

func runSendWhatsapp(cmd *cobra.Command, args []string) error {
	account, err := cmd.Flags().GetString("account")
	if err != nil {
		return err
	}
	message, err := cmd.Flags().GetString("message")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	contact, err := cmd.Flags().GetString("contact")
	if err != nil {
		return err
	}

	return commands.RunSend(commands.SendParams{
		Platform: "whatsapp",
		Account:  account,
		Contact:  contact,
		Message:  message,
		DryRun:   dryRun,
	})
}

// parsePostAt accepts either a Unix timestamp (all digits) or an ISO 8601
// datetime string, and returns the corresponding Unix timestamp.
func parsePostAt(s string) (int64, error) {
	// Pure digits → already a Unix timestamp.
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ts, nil
	}

	// Try common ISO 8601 layouts, most specific first.
	layouts := []string{
		time.RFC3339,          // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05", // no timezone → local
		"2006-01-02T15:04",    // no seconds
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Now().Location()); err == nil {
			return t.Unix(), nil
		}
	}

	return 0, fmt.Errorf("cannot parse --post-at %q: use ISO 8601 (e.g. 2026-04-11T09:00:00, local timezone unless offset given) or a Unix timestamp", s)
}
