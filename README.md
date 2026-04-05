# Pigeon

Pigeon is a local-first messaging bridge for AI agents. It mirrors your messaging history as plain text files and lets AI agents read, search, and reply to messages through a CLI, or receive them in real time through Claude Code.

## The Idea

Messaging platforms generate a constant stream of context that AI agents could act on, but these platforms are designed for humans, not programmatic access. Pigeon solves this by maintaining a local mirror of your messaging history and providing a CLI that agents can use to navigate and search that history.

When connected to Claude Code, pigeon delivers messages in real time via channel notifications. Claude can then react to incoming messages, gather context from other tools, and draft replies, all while keeping a human in the loop for anything outgoing.

This becomes powerful when combined with other agent-facing CLIs like [linear-cli](https://github.com/schpet/linear-cli) for issue tracking or [Google Workspace CLI](https://github.com/googleworkspace/cli) for docs and email. The agent can correlate a Slack question with a Linear ticket, pull context from a Google Doc, draft a reply, and queue it for your approval. Pigeon provides the messaging layer; the agent orchestrates across all of them.

## How It Works

**Listeners** connect to messaging platforms (Slack, WhatsApp) and append incoming messages to local text files, organized by date. They run as a background daemon.

**The CLI** lets agents (or humans) list conversations, read history, search across platforms, and send replies. `pigeon help` describes everything.

**Channels** deliver messages to Claude Code in real time via MCP channel notifications. When a new message arrives, Claude gets pinged and can react immediately.

**The Outbox** holds outgoing messages from Claude for human review. `pigeon review` opens a terminal UI to approve, reject, or provide feedback before anything is sent.

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
./install-skill.sh     # register Claude Code skill
```

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

Run `pigeon help` for the full list of commands.

## Multi-Account

Pigeon supports multiple Slack workspaces and WhatsApp numbers simultaneously. Each account gets its own listener, message store, and session binding. The daemon manages all of them, and `pigeon claude` lets you pick which account to connect.

## Architecture

```
┌─────────────┐     ┌─────────────┐
│    Slack     │     │  WhatsApp   │
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

The **daemon** runs all platform listeners, serves a Unix socket API, routes messages to connected Claude Code sessions, and manages the outbox.

The **hub** routes incoming messages to the right Claude Code session. One session per account. Handles session lifecycle: connect, disconnect, and handoff when a new session replaces an old one.

The **MCP server** runs inside Claude Code as a channel server. It receives messages from the daemon via SSE and delivers them as channel notifications that Claude can act on.
