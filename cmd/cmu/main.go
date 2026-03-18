package main

import (
	"fmt"
	"os"

	"github.com/anish/claude-msg-utils/internal/commands"
)

const usage = `cmu — messaging data CLI for AI agents

Reads locally-stored messaging data (WhatsApp, Slack, etc.) and provides
listeners that receive real-time messages and save them as text files.

DATA DIRECTORY

  Default: ~/.local/share/cmu/
  Override: set CMU_DATA_DIR environment variable

  Layout:

    <data-dir>/
    ├── whatsapp/
    │   ├── +14155551234/                  # account (phone number)
    │   │   ├── +14155559876_Alice/        # conversation (phone_name)
    │   │   │   ├── 2026-03-15.txt         # messages by date
    │   │   │   └── 2026-03-16.txt
    │   │   └── +14155550000_Bob/
    │   │       └── 2026-03-16.txt
    │   └── +919876543210/                 # second WhatsApp number
    │       └── ...
    ├── slack/
    │   ├── acme-corp/                     # workspace name
    │   │   ├── #engineering/              # channel
    │   │   │   └── 2026-03-16.txt
    │   │   └── @dave/                     # DM
    │   │       └── 2026-03-16.txt
    │   └── side-project/                  # second workspace
    │       └── ...

  Hierarchy: platform / account / conversation / YYYY-MM-DD.txt

  Message format (one per line):
    [2026-03-16 09:15:02] Alice: Hey, are you free?

COMMANDS — READING

  list              List platforms, accounts, or conversations
  read              Read messages from a conversation
  search            Search across conversations by keyword

COMMANDS — LISTENERS

  setup-whatsapp    Pair a WhatsApp device via QR code
  listen-whatsapp   Listen for WhatsApp messages and save to files
  listen-slack      Listen for Slack messages and save to files

OTHER

  help              Show this help

─────────────────────────────────────────────────────────

LIST

  cmu list
    Show all platforms and their accounts.

  cmu list -platform=whatsapp
    Show accounts for a specific platform.

  cmu list -platform=whatsapp -account=+14155551234
    Show conversations for a specific account.

  Options:
    -platform   Filter by platform name (whatsapp, slack, ...)
    -account    Filter by account name (phone number, workspace, ...)

READ

  cmu read -platform=whatsapp -account=+14155551234 -contact=Alice
    Read today's messages with Alice. Contact is matched by substring
    (case-insensitive) against directory name, display name, or identifier.

  cmu read -platform=whatsapp -account=+14155551234 -contact=Alice -date=2026-03-15
    Read messages from a specific date.

  cmu read -platform=slack -account=acme-corp -contact=#engineering -last=50
    Read the last 50 messages in a channel.

  cmu read -platform=whatsapp -account=+14155551234 -contact=Bob -since=2h
    Read messages from the last 2 hours.

  Options:
    -platform   Platform name [required]
    -account    Account name [required]
    -contact    Contact name, phone, or channel to find [required]
    -date       Specific date (YYYY-MM-DD)
    -last       Last N messages across all dates
    -since      Messages from last duration (e.g. 30m, 2h, 7d)

SEARCH

  cmu search -q="deploy"
    Search all platforms for "deploy".

  cmu search -q="bug" -platform=slack -account=acme-corp
    Search a specific workspace.

  cmu search -q="lunch" -since=7d
    Search only recent messages.

  Options:
    -q          Search query [required]
    -platform   Filter by platform
    -account    Filter by account
    -since      Only search messages from last duration (e.g. 2h, 7d)

─────────────────────────────────────────────────────────

SETUP-WHATSAPP

  Pair a new WhatsApp device by scanning a QR code. This stores device
  credentials in a local SQLite database and outputs the device JID
  needed for listen-whatsapp.

  cmu setup-whatsapp
    Pair using default database (~/.local/share/cmu/whatsapp.db).

  cmu setup-whatsapp -db=/path/to/whatsapp.db
    Pair using a custom database path.

  Options:
    -db   SQLite database path (default: <data-dir>/whatsapp.db)

  After pairing, you'll get a device JID like "14155551234:5@s.whatsapp.net".
  Use it with listen-whatsapp.

LISTEN-WHATSAPP

  Connect to WhatsApp and save incoming messages as text files.
  Long-running process — press Ctrl+C to stop.

  cmu listen-whatsapp -device=14155551234:5@s.whatsapp.net
    Listen using the paired device. Account directory defaults to
    the phone number from the JID (e.g. +14155551234).

  cmu listen-whatsapp -device=14155551234:5@s.whatsapp.net -account=personal
    Use a custom account directory name.

  Options:
    -device     Device JID from setup-whatsapp [required]
    -db         SQLite database path (default: <data-dir>/whatsapp.db)
    -account    Account label for directory name (default: +phone from JID)

LISTEN-SLACK

  Connect to Slack via Socket Mode and save incoming messages as text files.
  Long-running process — press Ctrl+C to stop.

  Requires:
    1. A Slack app with Socket Mode enabled
    2. An app-level token (xapp-...) with connections:write scope
    3. A bot token (xoxb-...) with channels:history, users:read, etc.

  cmu listen-slack -workspace=acme-corp -token=xapp-... -bot-token=xoxb-...
    Listen to a Slack workspace.

  Tokens can also be set via environment variables:
    SLACK_APP_TOKEN=xapp-...
    SLACK_BOT_TOKEN=xoxb-...

  cmu listen-slack -workspace=acme-corp
    Listen using tokens from environment variables.

  Options:
    -workspace   Workspace name for directory [required]
    -token       Slack app-level token (or SLACK_APP_TOKEN env var)
    -bot-token   Slack bot token (or SLACK_BOT_TOKEN env var)

─────────────────────────────────────────────────────────

AGENT WORKFLOW

  1. Discover what's available:
       cmu list

  2. Find a specific conversation:
       cmu list -platform=whatsapp -account=+14155551234

  3. Read recent messages:
       cmu read -platform=whatsapp -account=+14155551234 -contact=Alice -last=20

  4. Search for something specific:
       cmu search -q="meeting" -since=24h

SETUP WORKFLOW

  1. Pair your WhatsApp:
       cmu setup-whatsapp

  2. Start listening (in a separate terminal):
       cmu listen-whatsapp -device=<JID from step 1>

  3. Start listening to Slack (in another terminal):
       cmu listen-slack -workspace=my-company

  4. Now use the reader commands to browse messages.
`

func main() {
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
	case "setup-whatsapp":
		err = commands.RunSetupWhatsApp(args)
	case "listen-whatsapp":
		err = commands.RunListenWhatsApp(args)
	case "listen-slack":
		err = commands.RunListenSlack(args)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nRun 'cmu help' for usage.\n", cmd)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
