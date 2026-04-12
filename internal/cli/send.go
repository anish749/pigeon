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

Target flags are platform-specific:
  Slack:    --user-id (DMs) or --channel (channels, group DMs)
  WhatsApp: --contact (name or phone number)

By default, Slack messages are sent as the bot. Use --via pigeon-as-user to send as yourself.
Use --thread to reply to a thread, and --broadcast to also post the reply to the channel.
Run 'pigeon list' to find user IDs and channel names.`,
		Example: `  # Slack
  pigeon send -p slack -a acme-corp --user-id U07HF6KQ7PY -m "hey"
  pigeon send -p slack -a acme-corp --user-id U07HF6KQ7PY --via pigeon-as-user -m "sent as me"
  pigeon send -p slack -a acme-corp --channel '#engineering' -m "deploying now"
  pigeon send -p slack -a acme-corp --channel '#engineering' --thread 1711568938.123456 -m "fixed!"
  pigeon send -p slack -a acme-corp --channel '#engineering' --post-at 2026-04-11T09:00:00 -m "scheduled"
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
			postAt, err := cmd.Flags().GetString("post-at")
			if err != nil {
				return err
			}
			viaStr, err := cmd.Flags().GetString("via")
			if err != nil {
				return err
			}
			via := modelv1.Via(viaStr)
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
				if postAt != "" {
					return fmt.Errorf("--post-at is only supported for Slack")
				}
			}

			// Convert human-readable --post-at to Unix timestamp for the API.
			if postAt != "" {
				ts, err := parsePostAt(postAt)
				if err != nil {
					return err
				}
				postAt = strconv.FormatInt(ts, 10)
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
				PostAt:    postAt,
				Via:       via,
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
	cmd.Flags().String("post-at", "", "when to send: ISO 8601 (2026-04-11T09:00:00, local timezone) or Unix timestamp (Slack only, up to 120 days)")
	cmd.Flags().String("via", string(modelv1.ViaPigeonAsBot), "message pathway: pigeon-as-bot (default) or pigeon-as-user")
	cmd.Flags().Bool("dry-run", false, "validate without sending")
	cmd.Flags().Bool("force", false, "send even if the thread is not found locally")

	return cmd
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
