# Bugs / Tech Debt

## Implement maintenance in the daemon

This needs to run per account because the in case of WhatsApp the daemon may hold a setup lock, like the db lock is n needed.
So just think about the case when maintenance is running and there's a separate setup WhatsApp command running.
This means like only one of the things, either the daemon or the one of the clients needs to first gain the lock on that account and then they can work.

## Slack mentions are not being delivered to Claude

When someone mentions inside a thread uh or I don't know if it in case of other other states what happens, but plot didn't work.

## Dedup messages by last message id

## Validate date where calendar events are attributed

Right now we used a start date. This can be particularly problematic for multi day events.
We need to validate how exactly this works in practice.

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

## `pigeon read` does not work for GWS accounts

`pigeon read` now rejects GWS and Linear platforms with a clear error (#198) instead of silently returning wrong output. The underlying limitation remains — there is no supported way to read GWS data via `pigeon read`. The design for GWS read semantics is tracked as a feature.

## Slack: edits and deletes skipped when user_id is empty

The Slack listener skips `message_changed` and `message_deleted` events when the event carries an empty `user_id`. These are logged as warnings (`slack: skipping edit/delete, cannot resolve user`) but the edit or delete is silently dropped. This appears to happen for bot or app-authored messages where Slack omits the user field. The data on disk retains the original message with no indication it was edited or deleted.

## Slack: missing_scope errors on every sync cycle

Every sync cycle logs `failed to fetch muted channels, skipping mute filter` and `search failed: missing_scope`. The bot tokens lack the OAuth scope needed for muted-channel filtering and search. The daemon degrades gracefully — it syncs all channels instead of prioritizing unmuted ones — but the warnings are emitted on every cycle for every workspace, making the logs very noisy.

## Slack: "all messages filtered" after sync fetch

During sync, some channels fetch messages successfully but then filter out every single one, logging `slack sync: all messages filtered`. This likely happens in bot-only or integration-heavy channels where every message is from a bot or app that gets filtered. The channel appears empty on disk despite having real activity.

## WhatsApp: recurring websocket EOF disconnects

The WhatsApp listener logs `Error reading from websocket: failed to read frame header: EOF` several times per day. The connection recovers automatically, but each disconnect means a brief window of missed real-time events. If a message arrives during the reconnect gap it may not be captured until the next history sync (if one happens).

## WhatsApp: SQLITE_BUSY causes message decryption failures

When the WhatsApp database is locked (`database is locked (5) (SQLITE_BUSY)`), the listener fails to save a sender's push name and key material. This directly causes a subsequent decryption failure (`no sender key for ... in group`) — the group message is permanently lost because the key needed to decrypt it was never stored. This is a data-loss bug.

## Gmail: keyring backend stdout pollution causes poll failure

The GWS Gmail poller fails to fetch a message because the underlying `gws` CLI prints `Using keyring backend: keyring` to stdout, which gets mixed into the API response. The poller treats the corrupted response as an error. This means individual emails can be silently skipped during polling.

## Slack: send fails to MPDM channels with channel_not_found

`pigeon hub send` fails when targeting a multi-party DM (MPDM) channel, returning `channel_not_found`. The bot is not a member of the group DM and cannot join it programmatically. The outbox marks the message as failed on approve, but the user gets no actionable guidance beyond the raw error.

## Slack: @Slackbot DM fetch fails on every sync

Every sync cycle attempts to fetch the @Slackbot bot DM and fails with `channel_not_found`. The Slackbot DM channel cannot be fetched via the Slack API. This is expected behavior but it generates a warning on every sync cycle for every workspace, adding noise to the logs.

## Hub: reaction notifications lack context about the reacted message

When a reaction event is delivered to a connected Claude Code session via the hub, only the reaction itself is sent (emoji, channel, timestamp). The message that was reacted to is not included. The receiving session has no way to understand what the reaction means without separately looking up the original message. This makes reactions largely useless as a signal to the agent — it sees "someone reacted 👍" but not what they reacted to.
