package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/selfupdate"
)

// Command group IDs for categorized help output.
const (
	groupSetup       = "setup"
	groupDaemon      = "daemon"
	groupReading     = "reading"
	groupSending     = "sending"
	groupSlack       = "slack"
	groupMaintenance = "maintenance"
)

// ensureDaemon is a PreRunE hook for commands that benefit from the daemon running.
func ensureDaemon(cmd *cobra.Command, args []string) error {
	return daemon.EnsureRunning()
}

func newRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "pigeon",
		Short: "Messaging data CLI for AI agents",
		Long: `pigeon — messaging data CLI for AI agents

Reads locally-stored messaging data (WhatsApp, Slack, etc.) and provides
listeners that receive real-time messages and save them as JSONL files.

─────────────────────────────────────────────────────────

CONFIG

  Config file: ~/.config/pigeon/config.yaml (override: PIGEON_CONFIG_DIR)
  Data directory: ~/.local/share/pigeon/ (override: PIGEON_DATA_DIR)
  Daemon state: ~/.local/state/pigeon/ (override: PIGEON_STATE_DIR)

  The setup commands save listener credentials to config.yaml.
  The daemon reads this config to start all listeners at once.

  Example config.yaml:

    whatsapp:
      - device_jid: "14155551234:5@s.whatsapp.net"
        db: "~/.local/share/pigeon/whatsapp.db"
        account: "+14155551234"

    slack:
      - workspace: "acme-corp"
        app_token: "xapp-1-..."
        bot_token: "xoxb-..."

─────────────────────────────────────────────────────────

DATA LAYOUT

    <data-dir>/
    ├── whatsapp/
    │   ├── +14155551234/                  # account (phone number)
    │   │   ├── +14155559876_Alice/        # conversation (phone_name)
    │   │   │   └── 2026-03-16.jsonl
    │   │   └── +14155550000_Bob/
    │   │       └── 2026-03-16.jsonl
    ├── slack/
    │   ├── acme-corp/                     # workspace
    │   │   ├── #engineering/              # channel
    │   │   │   ├── 2026-03-16.jsonl
    │   │   │   └── threads/              # thread replies
    │   │   │       └── 1711568940.789012.jsonl
    │   │   └── @dave/                     # DM
    │   │       └── 2026-03-16.jsonl

  Hierarchy: platform / account / conversation / YYYY-MM-DD.jsonl
  Each file is JSONL — one JSON object per line, greppable with rg and jq.

  JSON fields:
    type      event type: "msg", "react", "unreact", "edit", "delete", "separator"
    ts        timestamp (ISO 8601, e.g. "2026-03-16T09:15:02Z")
    id        message ID (on msg events)
    msg       target message ID (on react/edit/delete events)
    sender    display name
    from      platform user ID (stable identity)
    text      message body (on msg/edit events)
    via       message pathway: "to-pigeon", "pigeon-as-user", "pigeon-as-bot"
    emoji     reaction emoji (on react/unreact events)
    attach    attachments array, each with "id" and "type" (MIME)
    reply     true if thread reply (on msg events)
    replyTo   quoted message ID (on msg events, WhatsApp quote-reply)

─────────────────────────────────────────────────────────

DIRECT FILE ACCESS — rg and jq

  The JSONL files are designed for direct access with ripgrep and jq.
  pigeon glob and pigeon grep wrap rg with --since filtering and
  thread awareness, but you can also use rg and jq directly:

    rg "deploy" ~/.local/share/pigeon/                                # all messages mentioning "deploy"
    rg "deploy" ~/.local/share/pigeon/slack/acme-corp/                # scoped to workspace

  Pipe to jq for structured queries:

    pigeon grep -q "deploy" -C 0 | cut -d: -f2- | jq 'select(.type == "msg")'
    pigeon grep -q "Alice" -C 0 | cut -d: -f2- | jq -r '"[" + .ts[11:19] + "] " + .sender + ": " + .text'
    pigeon glob --since=7d | xargs jq -r 'select(.type == "msg") | .sender'

  jq on a single file (no rg needed):

    jq 'select(.type == "msg")' ~/.local/share/pigeon/slack/acme-corp/#general/2026-03-16.jsonl
    jq -r 'select(.sender == "Alice") | .text' ~/.local/share/pigeon/slack/acme-corp/#general/2026-03-16.jsonl

─────────────────────────────────────────────────────────

WORKFLOW — FIRST-TIME SETUP

  WhatsApp:

    1. pigeon setup-whatsapp              # scan QR code
    2. pigeon daemon start                # starts listener in background

  Slack:

    1. pigeon generate-manifest --username=You --workspace=acme-corp
    2. Go to https://api.slack.com/apps → "Create New App" → "From a manifest"
    3. Paste the manifest from your clipboard
    4. Under "Basic Information", copy client ID and client secret
    5. Under "Socket Mode", enable it and create an app-level token (xapp-...)
    6. pigeon setup-slack
    7. Your browser opens — pick a workspace and approve
    8. pigeon daemon start                # starts listener in background

  Add more workspaces by running pigeon setup-slack again.

─────────────────────────────────────────────────────────

WORKFLOW — READING MESSAGES

  Discover what's available:

    pigeon list                             # all platforms and accounts
    pigeon list --platform=whatsapp         # accounts in a platform
    pigeon list --since=2h                  # conversations with recent activity
    pigeon glob --since=7d                  # find data files in a time window

  Read messages from a conversation:

    pigeon read --platform=whatsapp --account=+14155551234 --contact=Alice --last=20
    pigeon read --platform=slack --account=acme-corp --contact=#engineering --since=2h
    pigeon read --platform=whatsapp --account=+14155551234 --contact=Bob --date=2026-03-16

    Modes: --last=N (last N messages), --since=DURATION (e.g. 30m, 2h, 7d),
           --date=YYYY-MM-DD (specific day). Default: today's messages.

  Search across conversations (requires ripgrep):

    pigeon grep -q "meeting" --since=24h
    pigeon grep -q "deploy" --platform=slack
    pigeon grep -q "bug" -p slack -a acme-corp --since=7d -C 3
    pigeon grep -q "deploy" -l              # file paths only
    pigeon grep -q "deploy" -c              # match counts per file
    pigeon grep -q "deploy" -C 0 | cut -d: -f2- | jq 'select(.type == "msg")'

─────────────────────────────────────────────────────────

WORKFLOW — SENDING MESSAGES

  pigeon send -p whatsapp -a +14155551234 -c Alice -m "hey!"
  pigeon send -p slack -a acme-corp -c #engineering -m "deploying now"
  pigeon send -p slack -a acme-corp -c @alice -m "quick question"

  Slack messages are sent as the bot by default. Use --as-user to send as yourself.

  Thread replies:

    pigeon send -p slack -a acme-corp -c #engineering --thread 1711568938.123456 -m "done"
    pigeon send -p slack -a acme-corp -c #engineering --thread 1711568938.123456 --broadcast -m "resolved"

  If your Slack app was installed before bot sending was added,
  re-run 'pigeon setup-slack' to update scopes.

─────────────────────────────────────────────────────────

DAEMON

  The daemon runs in the background and syncs messages in real-time.
  It starts automatically when you use read, send, search, or list.

  pigeon daemon start                     # start manually
  pigeon daemon stop                      # stop
  pigeon daemon restart                   # restart
  pigeon daemon status                    # check status

  Logs: ~/.local/state/pigeon/daemon.log

─────────────────────────────────────────────────────────

MAINTENANCE

  pigeon reset --platform=slack --account=acme-corp

  Deletes all synced message data and sync cursors for a workspace/account.
  The next daemon start will re-sync from scratch.`,
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			slog.SetDefault(slog.New(tint.NewHandler(os.Stdout, &tint.Options{
				Level:      slog.LevelInfo,
				TimeFormat: time.Kitchen,
			})))
			selfupdate.AutoCheck(version)
			if _, err := exec.LookPath("rg"); err != nil {
				return fmt.Errorf("ripgrep (rg) is required but not found on PATH — install it: https://github.com/BurntSushi/ripgrep#installation")
			}
			return nil
		},
	}

	root.AddGroup(
		&cobra.Group{ID: groupSetup, Title: "Setup:"},
		&cobra.Group{ID: groupDaemon, Title: "Daemon:"},
		&cobra.Group{ID: groupReading, Title: "Reading:"},
		&cobra.Group{ID: groupSending, Title: "Sending:"},
		&cobra.Group{ID: groupSlack, Title: "Slack:"},
		&cobra.Group{ID: groupMaintenance, Title: "Maintenance:"},
	)

	root.AddCommand(
		// Setup
		newSetupWhatsAppCmd(),
		newSetupSlackCmd(),

		// Daemon
		newDaemonCmd(version),
		newClaudeSessionCmd(),
		newMCPCmd(),
		newLogCmd(),

		// Reading
		newListCmd(),
		newReadCmd(),
		newGlobCmd(),
		newGrepCmd(),

		// Sending
		newSendCmd(),
		newReactCmd(),
		newReviewCmd(),

		// Slack
		newGenerateManifestCmd(),

		// Maintenance
		newResetCmd(),
		newUnlinkWhatsAppCmd(),
		newUpdateCmd(version),
	)

	return root
}

// Execute runs the root command.
func Execute(version string) error {
	return newRootCmd(version).Execute()
}
