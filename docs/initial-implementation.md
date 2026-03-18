# Pigeon — Initial Implementation

This document describes what has been built so far and the decisions behind it, so that someone continuing this work understands the current state without needing to re-derive the reasoning.

## What Exists

### Reader CLI

Three commands are implemented: one to list what's available (platforms, accounts, conversations), one to read messages from a specific conversation, and one to search across all conversations by keyword.

The list command supports progressive drill-down — calling it with no arguments shows all platforms, adding a platform filter shows accounts, adding an account shows conversations. This mirrors how an agent would explore: broad to narrow.

The read command supports filtering by date, by recency (last N messages), or by time window (last 2 hours). Contact lookup is fuzzy — substring match against the conversation directory name, display name, or identifier. If no filter is specified, it defaults to today's messages.

The search command does a brute-force scan across all text files, optionally scoped by platform, account, or time window. There's no index — it just reads and greps. This is fine for personal messaging volumes.

### WhatsApp Listener

The WhatsApp listener uses the whatsmeow library, which implements the WhatsApp Web protocol over WebSocket. Device pairing happens via QR code — the setup command displays a QR in the terminal, the user scans it with their phone, and the device credentials are stored in a local SQLite database.

The listener handles a nuance of WhatsApp's protocol: newer versions use "Linked IDs" (LIDs) instead of phone-number-based JIDs for sender identification. The listener resolves LIDs back to phone numbers so that conversation directories are consistently named by phone number.

Message text is extracted from several WhatsApp message types — plain text, extended text (messages with link previews), and media captions (images, videos, documents that have text attached). Media files themselves are not downloaded — only the text content is preserved.

Conversation directories are named as a combination of the sender's phone number and their WhatsApp display name (push name). This means the reader's fuzzy search can match on either.

### Slack Listener

The Slack listener uses Socket Mode, which establishes a WebSocket from the local machine to Slack's servers. This avoids needing a public URL — everything runs locally.

On startup, the listener preloads a cache of user ID to display name mappings and channel ID to channel name mappings from the Slack API. This is because Slack events only contain opaque IDs — the cache translates them to human-readable names for the text files. Cache misses (new users, new channels) fall back to individual API lookups.

The listener filters out bot messages and message subtypes (edits, deletes, join/leave notifications) — only new human-authored messages are persisted.

### Slack OAuth for Multi-Workspace

The Slack app is designed to be installable across multiple workspaces. Each workspace installation produces a separate bot token, while the app-level token (used for Socket Mode) is shared.

The OAuth flow runs on a local HTTPS server. HTTPS is required because Slack mandates it for redirect URLs in distributed apps. The certificates are generated using mkcert, which creates locally-trusted certificates — the setup command handles installing mkcert and generating certs automatically.

When the user runs the setup command, it opens the browser to Slack's authorization page. The user picks a workspace and approves. Slack redirects back to the local HTTPS callback, which exchanges the authorization code for a bot token and saves it to the config. For workspaces the user doesn't own, Slack's "Activate Public Distribution" setting needs to be enabled on the app — the setup command explains this.

The daemon also runs this OAuth server, so new workspaces can be added at runtime without restarting. When a new workspace is installed via OAuth callback, the daemon immediately spins up a listener for it.

### Daemon

The daemon reads the config file and starts all configured listeners concurrently in a single process. WhatsApp listeners connect via WebSocket to WhatsApp's servers. Slack listeners connect via Socket Mode. Errors from individual listeners are logged but don't crash the process — if one workspace's token expires, the others keep running.

### Configuration

There are two configuration concerns:

**App-level credentials** (Slack client ID, client secret, app-level token) are stored once and shared across all workspaces. These are provided during first-time setup.

**Per-workspace/per-device credentials** (Slack bot tokens obtained via OAuth, WhatsApp device JIDs) are accumulated over time as the user adds more accounts. Reinstalling the same Slack workspace (identified by team ID) overwrites the previous entry rather than creating a duplicate.

### Skill Registration

The CLI is designed to be discovered by AI agents through a skill definition that tells the agent where the binary is and instructs it to read the help output before doing anything else. An install script hydrates a template with the actual binary path and places it where the agent framework can find it.

## What Doesn't Exist Yet

- **Sending messages.** The CLI is read-only. An agent can read conversations but cannot reply through Pigeon.
- **Media handling.** Images, files, voice messages, and stickers are silently dropped. Only text and captions are captured.
- **Group chat awareness.** WhatsApp group messages are not handled differently from individual chats. The conversation directory is based on the chat JID, which for groups would be the group ID rather than an individual contact.
- **Message edits and deletions.** If someone edits or deletes a message on the platform, the original text remains in the file. The files are append-only — there is no mechanism to retroactively modify them.
- **Historical backfill.** The listeners only capture messages received while they are running. There is no import of historical messages from before Pigeon was set up.
- **Platforms beyond WhatsApp and Slack.** The architecture supports adding more, but only these two have listener implementations.
