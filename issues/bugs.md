# Bugs / Tech Debt

## Implement maintenance in the daemon

This needs to run per account because the in case of WhatsApp the daemon may hold a setup lock, like the db lock is n needed.
So just think about the case when maintenance is running and there's a separate setup WhatsApp command running.
This means like only one of the things, either the daemon or the one of the clients needs to first gain the lock on that account and then they can work.

## Slack mentions are not being delivered to Claude

When someone mentions inside a thread uh or I don't know if it in case of other other states what happens, but plot didn't work.

## Thread files cannot be date-filtered without scanning content

Thread files are stored at `<conversation>/threads/<ts>.jsonl` where `<ts>` is
the thread root timestamp, not the last-modified date. When a user runs
`pigeon glob --since` or `pigeon grep --since`, the filename carries no
information about whether the thread has had recent activity.

As a result, `Glob --since` falls back to a content scan via
`rgFilesWithContent(ThreadGlobRg, threadDatePatterns(since))`: for every thread
file under the data root, rg opens the file and searches for `"ts":"YYYY-MM-DD`
prefixes inside the JSONL to decide whether the thread intersects the window.

Two problems with this:

- **Cost scales with total thread history**, not with recency. Every
  `--since` query re-opens and re-scans every thread file under the root,
  regardless of whether the thread has been touched in months.
- **The date filter is coupled to modelv1's JSONL serialization**. The match
  pattern `"ts":"YYYY-MM-DD` depends on the field name, JSON key order, and
  date format staying exactly what modelv1 currently produces. The doc
  comment on `threadDatePatterns` already warns about this — any change to
  how `ts` is serialized breaks the date filter silently.

This only affects the `--since` path. Without `--since`, thread files come in
via the plain `*.jsonl` glob and neither of the above matters.

Note: #200 improved this by including thread replies when the parent is outside the date range, but the fundamental content-scanning cost remains.

## Storage is implicitly UTC — not documented

Pigeon stores data in date-partitioned files (gmail, calendar, messaging,
drive content) whose filenames encode a `YYYY-MM-DD` date. The read layer's
`--since` filter computes its window in UTC — `dateGlobs`,
`threadDatePatterns`, and `DriveMetaFileGlobsSince` all call
`time.Now().UTC().Truncate(24*time.Hour)` — so for the filter to return the
right files, every writer that names a file by date has to be using UTC too.

This is the intended convention but it is implicit. It is not mentioned in
`pigeon` help, not documented anywhere a user would see, and nothing in the
code guards against a future writer filing a date in local time. A user
whose machine is in a non-UTC zone has no way to know that "yesterday's"
messages may land in today's file, or vice versa, depending on how the
write path handled the clock.

## Slack: missing_scope errors on every sync cycle

Every sync cycle logs `failed to fetch muted channels, skipping mute filter` and `search failed: missing_scope`. The bot tokens lack the OAuth scope needed for muted-channel filtering and search. The daemon degrades gracefully — it syncs all channels instead of prioritizing unmuted ones — but the warnings are emitted on every cycle for every workspace, making the logs very noisy.

## WhatsApp: recurring websocket EOF disconnects

The WhatsApp listener logs `Error reading from websocket: failed to read frame header: EOF` several times per day. The connection recovers automatically, but each disconnect means a brief window of missed real-time events. If a message arrives during the reconnect gap it may not be captured until the next history sync (if one happens).

## WhatsApp: SQLITE_BUSY causes message decryption failures

When the WhatsApp database is locked (`database is locked (5) (SQLITE_BUSY)`), the listener fails to save a sender's push name and key material. This directly causes a subsequent decryption failure (`no sender key for ... in group`) — the group message is permanently lost because the key needed to decrypt it was never stored. This is a data-loss bug.

## Platform names: rename `linear-issues` / `jira-issues` to `linear` / `jira`

The platform names `linear-issues` and `jira-issues` are confusing — the "-issues" suffix reads like a sub-resource of the platform rather than the platform itself. They should be `linear` and `jira`, matching how the other platforms (`slack`, `whatsapp`) are named.

## Slack: edits to messages aren't delivered to the agent in real time

When a Slack message is edited, the slack listener writes an edit line to disk but no notification reaches the connected Claude session. `handleEdit` in `internal/listener/slack/listener.go` calls `messages.AppendEdit(...)` and stops — there is no callback to the hub, so the hub never learns an edit happened.

## Slack: deletes to messages aren't delivered to the agent in real time

Symmetric to the edit bug above. `handleDelete` writes a delete line to disk and stops; no signal to the hub, so the connected Claude session never sees the delete.

## Slack: notification format doesn't carry thread context

The channel notification delivered to the agent for a thread reply contains the message id, sender id, channel type, and channel id, but no `thread_ts`. From the payload alone, the agent cannot tell that the message is a reply, which thread it belongs to, what the parent message was, or whether to thread its own reply back into the same thread.

Reactions should be subject to the same treatment — when a reaction lands on a thread message, the formatted notification should also carry `thread_ts` so the agent knows where the reaction happened. (Reaction delivery itself works post-#322; only the format payload is missing thread context.)

## Slack: edits/deletes on thread replies persisted to the wrong file

Edits and deletes that target a thread reply are persisted to the channel date file rather than the thread file. As a result, neither `Compact()` nor `CompactThread()` apply them — a `pigeon read` after the edit still shows the original text.

## Slack write phase: blocks stored even when identical to text fallback

In the Slack write phase, blocks are stored raw even when the text fallback is the exact same representation of what is in the blocks. When the text and blocks carry the same content, writing the blocks adds no additional value.

Deduplication should happen during write, since at write time we already know whether the blocks add anything beyond the text. Doing this merge during read does not make sense — if there is no additional value, there is no reason to store it in the first place.


