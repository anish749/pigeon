package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"

	"github.com/anish/claude-msg-utils/internal/commands"
)

const usage = `pigeon — messaging data CLI for AI agents

Reads locally-stored messaging data (WhatsApp, Slack, etc.) and provides
listeners that receive real-time messages and save them as text files.

COMMANDS — SETUP

  setup-whatsapp    Pair a WhatsApp device via QR code, save to config
  setup-slack       Save Slack workspace credentials to config

COMMANDS — DAEMON

  daemon start      Start all configured listeners

COMMANDS — READING

  list              List platforms, accounts, or conversations
  read              Read messages from a conversation
  search            Search across conversations by keyword

COMMANDS — SENDING

  send              Send a message (requires daemon to be running)

COMMANDS — SLACK

  generate-manifest Generate a Slack app manifest for a workspace

COMMANDS — MAINTENANCE

  reset             Delete all synced data for a platform/account

OTHER

  help              Show this help

─────────────────────────────────────────────────────────

CONFIG

  Config file: ~/.config/pigeon/config.yaml (override: PIGEON_CONFIG_DIR)
  Data directory: ~/.local/share/pigeon/ (override: PIGEON_DATA_DIR)

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

GENERATE-MANIFEST

  pigeon generate-manifest -username=Anish -workspace=acme-corp

  Renders the Slack app manifest template (manifests/slack-app.yaml) with
  the given username and workspace name, prints it to stdout, and copies
  it to the clipboard. Use this before creating or updating a Slack app.

  Options:
    -username    Display name for the bot owner [required]
    -workspace   Slack workspace name [required]

─────────────────────────────────────────────────────────

SETUP-WHATSAPP

  pigeon setup-whatsapp
    Pair using default database (~/.local/share/pigeon/whatsapp.db).

  pigeon setup-whatsapp -db=/path/to/whatsapp.db
    Pair using a custom database path.

  Options:
    -db   SQLite database path (default: <data-dir>/whatsapp.db)

SETUP-SLACK

  Installs the Slack app in a workspace via OAuth. Opens your browser
  to Slack's authorization page — pick a workspace and approve.

  First time (provide app credentials):

    pigeon setup-slack -client-id=12345.67890 -client-secret=abc123 -app-token=xapp-1-...

  Subsequent workspaces (credentials already saved):

    pigeon setup-slack

  Options:
    -client-id       Slack app client ID (first time only, or SLACK_CLIENT_ID)
    -client-secret   Slack app client secret (first time only, or SLACK_CLIENT_SECRET)
    -app-token       Slack app-level token (first time only, or SLACK_APP_TOKEN)

  To create a Slack app:
    1. Run: pigeon generate-manifest -username=You -workspace=acme-corp
    2. Go to https://api.slack.com/apps → "Create New App" → "From a manifest"
    3. Paste the manifest from your clipboard
    4. Under "Basic Information", copy client ID and client secret
    5. Under "Socket Mode", enable it and create an app-level token (xapp-...)
    6. Run: pigeon setup-slack -client-id=... -client-secret=... -app-token=...
    7. Your browser opens — pick a workspace and approve
    8. Done! Add more workspaces by running: pigeon setup-slack

DAEMON

  pigeon daemon start
    Start all configured listeners. Also runs a local HTTP server on
    port 9876 for adding new Slack workspaces at runtime via:
      http://localhost:9876/slack/install
    Runs until Ctrl+C.

─────────────────────────────────────────────────────────

LIST

  pigeon list
  pigeon list -platform=whatsapp
  pigeon list -platform=whatsapp -account=+14155551234

  Options:
    -platform   Filter by platform name
    -account    Filter by account name

READ

  pigeon read -platform=whatsapp -account=+14155551234 -contact=Alice
  pigeon read -platform=slack -account=acme-corp -contact=#engineering -last=50
  pigeon read -platform=whatsapp -account=+14155551234 -contact=Bob -since=2h

  Options:
    -platform   Platform name [required]
    -account    Account name [required]
    -contact    Contact name, phone, or channel to find [required]
    -date       Specific date (YYYY-MM-DD)
    -last       Last N messages across all dates
    -since      Messages from last duration (e.g. 30m, 2h, 7d)

SEARCH

  pigeon search -q="deploy"
  pigeon search -q="bug" -platform=slack -account=acme-corp
  pigeon search -q="lunch" -since=7d

  Options:
    -q          Search query [required]
    -platform   Filter by platform
    -account    Filter by account
    -since      Only search messages from last duration (e.g. 2h, 7d)

SEND

  pigeon send -platform=whatsapp -account=+14155551234 -contact=Alice -m "hey, are you free?"
  pigeon send -platform=slack -account=acme-corp -contact=#engineering -m "deploying now"

  Sends a message through the daemon's connected clients. The daemon must
  be running (pigeon daemon start) for this to work.

  Options:
    -platform   Platform name [required]
    -account    Account name [required]
    -contact    Contact name, phone, or channel [required]
    -m          Message text [required]

  Note: Slack sending requires chat:write scope. If your Slack app was
  installed before this feature, re-run 'pigeon setup-slack' to update scopes.

RESET

  pigeon reset -platform=slack -account=acme-corp

  Deletes all synced message data and sync cursors for a workspace/account.
  The next daemon start will re-sync from scratch.

  Options:
    -platform   Platform name [required]
    -account    Account/workspace name [required]

─────────────────────────────────────────────────────────

WORKFLOW

  First-time setup:

    1. pigeon setup-whatsapp          # scan QR code
    2. pigeon setup-slack -workspace=acme-corp -token=... -bot-token=...
    3. pigeon daemon start            # starts all listeners

  Reading messages (from a different terminal or agent):

    1. pigeon list                    # see what's available
    2. pigeon read -platform=whatsapp -account=+14155551234 -contact=Alice -last=20
    3. pigeon search -q="meeting" -since=24h
`

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.Kitchen,
	})))

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "list":
		err = commands.RunList(args)
	case "read":
		err = commands.RunRead(args)
	case "search":
		err = commands.RunSearch(args)
	case "send":
		err = commands.RunSend(args)
	case "setup-whatsapp":
		err = commands.RunSetupWhatsApp(args)
	case "setup-slack":
		err = commands.RunSetupSlack(args)
	case "daemon":
		err = commands.RunDaemon(args)
	case "reset":
		err = commands.RunReset(args)
	case "reset-whatsapp":
		err = commands.RunResetWhatsApp(args)
	case "generate-manifest":
		err = commands.RunGenerateManifest(args)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nRun 'pigeon help' for usage.\n", cmd)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
