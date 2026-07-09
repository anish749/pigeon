# Pigeon

Pigeon is a local-first bridge between AI agents and your messaging and workspace tools. It mirrors Slack and WhatsApp conversations, Gmail, Calendar, and Drive activity, and Linear and Jira issues as plain text files, and lets AI agents read, search, and reply through a CLI — with Slack and WhatsApp messages also delivered in real time through Claude Code.

## The Idea

Messaging and workspace platforms generate a constant stream of context that AI agents could act on, but these platforms are designed for humans, not programmatic access. Pigeon solves this by maintaining a local mirror of that history — Slack and WhatsApp conversations, Gmail/Calendar/Drive activity, Linear and Jira issues — and providing a CLI that agents can use to navigate and search across all of it.

When connected to Claude Code, pigeon delivers Slack and WhatsApp messages in real time via channel notifications. Claude can then react to an incoming message, pull context from a related Linear ticket, Jira issue, or Google Doc, draft a reply, and queue it for your approval, all while keeping a human in the loop for anything outgoing.

Pigeon builds on top of [linear-cli](https://github.com/schpet/linear-cli), [jira-cli](https://github.com/ankitpokhrel/jira-cli), and the [Google Workspace CLI](https://github.com/googleworkspace/cli): you authenticate through those tools, and pigeon uses that same auth to poll and mirror your issues and workspace activity locally.

## How It Works

**Listeners** connect to messaging platforms (Slack, WhatsApp) and append incoming messages to local text files, organized by date. They run as a background daemon.

**Pollers** sync Google Workspace (Gmail, Calendar, Drive), Linear, and Jira on a schedule, mirroring issues and workspace activity to local files the same way listeners mirror messages.

**The CLI** lets agents (or humans) list conversations, read history, search across platforms, and send replies. `pigeon help` describes everything.

**Channels** deliver Slack and WhatsApp messages to Claude Code in real time via MCP channel notifications. When a new message arrives, Claude gets pinged and can react immediately. Workspace and issue data from pollers is searched on demand rather than pushed live.

**The Outbox** holds outgoing messages from Claude for human review. By default, review happens right in Slack: the daemon DMs you the pending message with buttons to approve, dismiss, or leave feedback. `pigeon review` opens a terminal UI for the same flow.

## Prerequisites

- **[ripgrep](https://github.com/BurntSushi/ripgrep#installation)** (`rg`) — required for search, read, and file discovery. Install via `brew install ripgrep`, `apt install ripgrep`, or see the [ripgrep installation guide](https://github.com/BurntSushi/ripgrep#installation).
- **[uv](https://docs.astral.sh/uv/getting-started/installation/)** — required for the embedding sidecar used by workstream routing. Install via `curl -LsSf https://astral.sh/uv/install.sh | sh`.

Optional, only needed for the integrations you use:

- **[Google Workspace CLI](https://github.com/googleworkspace/cli)** (`gws`) — for `pigeon setup-gws` (Gmail, Calendar, Drive). Run `gws auth login` first.
- **[linear-cli](https://github.com/schpet/linear-cli)** — for `pigeon setup-linear`. Run `linear auth login` first.
- **[jira-cli](https://github.com/ankitpokhrel/jira-cli)** — for `pigeon setup-jira`. Run `jira init` and export `JIRA_API_TOKEN` first.

## Installation

Download the latest release for your platform:

```bash
curl -fsSL https://raw.githubusercontent.com/anish749/pigeon/main/install.sh | bash
```

This detects your OS and architecture, downloads the binary from GitHub Releases, installs it to `~/.local/bin`, and registers the pigeon skill for Claude Code.

To install from source:

```bash
git clone https://github.com/anish749/pigeon.git
cd pigeon
./pigeon help          # wrapper script auto-builds
```

To register pigeon as a Claude Code skill:

```bash
npx skills add anish749/pigeon
```

Or manually: copy `.claude/skills/pigeon/SKILL.md` from this repo into `~/.claude/skills/pigeon/SKILL.md`.

## Getting Started

Set up a messaging platform, start the daemon, and connect Claude Code:

```bash
# Slack
pigeon setup-slack        # interactive OAuth flow
pigeon daemon start
pigeon claude              # select account, launches Claude Code with pigeon connected

# WhatsApp
pigeon setup-whatsapp      # scan QR code
pigeon daemon start
pigeon claude
```

Set up Google Workspace, Linear, or Jira to mirror them for search (no live Claude Code delivery):

```bash
pigeon setup-gws                    # after `gws auth login`
pigeon setup-linear                 # after `linear auth login`
pigeon setup-jira                   # after `jira init` + JIRA_API_TOKEN
pigeon daemon start
```

Run `pigeon help` for the full list of commands.

## Multi-Account

Pigeon supports multiple accounts per platform — several Slack workspaces, WhatsApp numbers, Google Workspace accounts, Linear workspaces, and Jira configurations simultaneously. Each gets its own store under the data root, and the `--workspace` flag scopes CLI operations to a subset of accounts. Slack and WhatsApp accounts additionally get a session binding: the daemon manages all of them, and `pigeon claude` lets you pick which one to connect.

## Architecture

```
┌─────────────┐     ┌─────────────┐
│    Slack    │     │  WhatsApp   │
└──────┬──────┘     └──────┬──────┘
       │                   │
       ▼                   ▼
┌──────────────────────────────────┐
│         Pigeon Daemon            │
│  listeners · hub · outbox · API  │
└───────┬──────────┬───────────────┘
        │          │
        ▼          ▼
   Text Files   Claude Code
   (storage)    (via MCP channel)
```

The **daemon** runs all platform listeners and pollers, serves a Unix socket API, routes Slack/WhatsApp messages to connected Claude Code sessions, and manages the outbox.

The **hub** routes incoming Slack and WhatsApp messages to the right Claude Code session. One session per account. Handles session lifecycle: connect, disconnect, and handoff when a new session replaces an old one.

The **MCP server** runs inside Claude Code as a channel server. It receives messages from the daemon via SSE and delivers them as channel notifications that Claude can act on.

Google Workspace, Linear, and Jira pollers run inside the same daemon and write to the same text-file storage on a schedule, but they don't go through the hub — that real-time push to Claude Code is Slack/WhatsApp only. Their history is searched on demand via the CLI instead.
