package cli

import (
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"

	"github.com/anish/claude-msg-utils/internal/daemon"
)

var rootCmd = &cobra.Command{
	Use:   "pigeon",
	Short: "Messaging data CLI for AI agents",
	Long: `pigeon — messaging data CLI for AI agents

Reads locally-stored messaging data (WhatsApp, Slack, etc.) and provides
listeners that receive real-time messages and save them as text files.

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
    │   │   │   └── 2026-03-16.txt
    │   │   └── +14155550000_Bob/
    │   │       └── 2026-03-16.txt
    ├── slack/
    │   ├── acme-corp/                     # workspace
    │   │   ├── #engineering/              # channel
    │   │   │   └── 2026-03-16.txt
    │   │   └── @dave/                     # DM
    │   │       └── 2026-03-16.txt

  Hierarchy: platform / account / conversation / YYYY-MM-DD.txt
  Message format: [2026-03-16 09:15:02] Alice: Hey, are you free?

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

  pigeon list                             # see what's available
  pigeon list --platform=whatsapp         # filter by platform

  pigeon read --platform=whatsapp --account=+14155551234 --contact=Alice --last=20
  pigeon read --platform=slack --account=acme-corp --contact=#engineering --since=2h

  pigeon search -q="meeting" --since=24h
  pigeon search -q="deploy" --platform=slack

─────────────────────────────────────────────────────────

WORKFLOW — SENDING MESSAGES

  pigeon send --platform=whatsapp --account=+14155551234 --contact=Alice -m "hey!"
  pigeon send --platform=slack --account=acme-corp --contact=#engineering -m "deploying now"

  Note: Slack sending requires chat:write scope. If your Slack app was
  installed before this feature, re-run 'pigeon setup-slack' to update scopes.

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
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		slog.SetDefault(slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelInfo,
			TimeFormat: time.Kitchen,
		})))
	},
}

// ensureDaemon is a PreRunE hook for commands that benefit from the daemon running.
func ensureDaemon(cmd *cobra.Command, args []string) error {
	return daemon.EnsureRunning()
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
