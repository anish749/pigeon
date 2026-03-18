# Pigeon — Concept

Pigeon is a local-first messaging bridge for AI agents.

The core idea is simple: messaging platforms generate a constant stream of conversations that an AI agent might need to reference, but these platforms are designed for humans — not for programmatic access by agents. Pigeon solves this by maintaining a local mirror of your messaging history as plain text files, and providing a CLI that agents can use to navigate and search that history.

## The Two Halves

Pigeon has two distinct responsibilities that are intentionally decoupled:

**Writers** are long-running listeners that connect to messaging platforms and append incoming messages to local text files. Each platform has its own listener implementation. A writer's only job is to receive events and persist them — it has no knowledge of who or what will read the data later.

**Readers** are stateless CLI commands that navigate the local text file structure. They let an agent list what conversations exist, read messages from a specific conversation, or search across all conversations for a keyword. A reader has no knowledge of where the data came from — it only knows the file layout.

The only contract between writers and readers is the directory structure and the message line format. This is what makes the system extensible: adding a new platform means writing a new listener that follows the same file conventions. The reader commands work automatically with any platform's data.

## Why Text Files

Messages are stored as append-only text files organized by date. This is a deliberate choice:

- **Agents can read text files natively.** Even without the CLI, an agent with file access could grep through the data directly. The CLI just makes it faster and more structured.
- **No database, no server, no state.** The file system is the database. There's nothing to migrate, nothing to back up beyond the files themselves.
- **Append-only writes are safe.** Multiple listeners can write concurrently without coordination. The OS guarantees atomic appends.
- **Human-readable by default.** You can open any file in a text editor and read the conversation. No decoding, no tooling required.

## Multi-Account by Design

A person may have multiple phone numbers on WhatsApp and belong to multiple Slack workspaces. The directory hierarchy reflects this: every conversation is nested under its platform and account, so there is never ambiguity about which identity a message belongs to.

## The Agent's Perspective

An AI agent using Pigeon doesn't need to understand messaging protocols, OAuth flows, or WebSocket connections. From the agent's perspective, Pigeon is a read-only CLI that answers questions like:

- What messaging platforms and accounts are available?
- What did Alice say on WhatsApp today?
- Has anyone mentioned "deploy" in the last 24 hours?

The agent discovers the CLI's capabilities by reading its help output, then chains commands together to find what it needs. This follows the same self-documenting pattern used by other agent-facing CLI tools.

## The Daemon

All listeners run as a single long-lived process. This process also serves a local HTTPS endpoint that handles OAuth callbacks, so adding a new Slack workspace is as simple as visiting a URL and picking the workspace — the daemon picks up the new credentials and starts listening immediately, without a restart.

## Platform Independence

The architecture is deliberately platform-agnostic at the storage layer. WhatsApp and Slack are just the first two implementations. Any messaging source that can push events — email, Discord, Telegram, SMS — could be added as a new listener without changing the reader commands or the storage format. The listener just needs to know how to connect to the platform, extract the sender, text, and timestamp, and write a line to the right file.
