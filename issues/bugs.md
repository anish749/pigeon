# Bugs / Tech Debt

## Delete sent Slack messages / unreact via CLI

`pigeon hub send` and `pigeon react` have no inverse — there is no CLI command to delete a message the bot sent or to remove a reaction it added, leaving the user with no way to undo those actions from the same interface.

## Expose protocol layer models to the user via CLI

Currently the flags used in the protocol layer and the the ones in the CLI are different.
The protocol is probably known and that should be the one that is referenced by others as well, including in the CLI, they should speak the same language.
 via := modelv1.ViaPigeonAsUser
 if !req.AsUser {
  via = modelv1.ViaPigeonAsBot
 }
## WhatsApp history not re-synced after reset

After `pigeon reset --platform=whatsapp`, the daemon reconnects to WhatsApp but does not receive a full history sync. WhatsApp only sends history sync events during initial device pairing. After a reset (where message data is deleted but the device stays paired), there is no mechanism to request a re-transfer of history.

The whatsmeow library may support requesting a history re-sync via a specific protocol message, but pigeon does not send one. This means after a reset, only new real-time messages are captured — all historical data is lost until the device is fully unpaired and re-paired via QR code.

**Workaround:** Run `pigeon unlink-whatsapp` then `pigeon setup-whatsapp` to re-pair, which triggers a full history sync.

**Files affected:** `internal/listener/whatsapp/listener.go`, `internal/listener/whatsapp/historysync.go`, `internal/daemon/whatsapp_manager.go`.

## Implement maintenance in the daemon

This needs to run per account because the in case of WhatsApp the daemon may hold a setup lock, like the db lock is n needed.
So just think about the case when maintenance is running and there's a separate setup WhatsApp command running.
This means like only one of the things, either the daemon or the one of the clients needs to first gain the lock on that account and then they can work.

## Slack mentions are not being delivered to Claude

When someone mentions inside a thread uh or I don't know if it in case of other other states what happens, but plot didn't work.

## Send reactions to Claude Code sessions

## Dedup messages by last message id

## Daemon has no restart/recovery for crashed accounts

All per-account goroutines (Slack listeners, WhatsApp listeners, GWS pollers) are fire-and-forget. If one crashes or returns an error, it logs and the goroutine exits permanently. Nothing restarts it — the account is dead until the entire daemon is restarted.

This affects all platforms equally:

- **Slack**: `smClient.RunContext` and `listener.Run` run in goroutines. If either exits, the account stops receiving messages.
- **WhatsApp**: same pattern — goroutine exits on error, no recovery.
- **GWS**: `p.Run(child)` exits on error, poller is gone.

The Slack socket mode library handles network-level reconnection internally, but if the goroutine itself panics or the library gives up, pigeon doesn't recover.

**Files affected:** `internal/daemon/slack_manager.go`, `internal/daemon/whatsapp_manager.go`, `internal/daemon/gws_manager.go`.

## Slack send does not resolve mentions

When Pigeon sends a Slack message, @mentions are not resolved — they appear as raw text instead of being converted to Slack's mention format. Recipients see plain-text "@name" that doesn't ping or link to the mentioned user.

## GWS: no maintenance/compaction for JSONL files

The protocol spec describes dedup and compaction for GWS JSONL files (emails, calendar events), and `compact.DedupGWS` exists for dedup. But the existing maintenance pass (`FSStore.Maintain`) only handles messaging data (`modelv1` lines). GWS JSONL files are never compacted. Gmail and calendar JSONL files accumulate duplicate event updates that are never cleaned up.

This also needs to apply pending email deletes (`.pending-email-deletes`) as part of the gmail maintenance pass.

**Files affected:** `internal/store/modelv1/compact/compact.go`, `internal/store/fs.go`.

## GWS Gmail: email deletes should be applied during maintenance

The poller writes deleted message IDs to `.pending-email-deletes` (#159). Maintenance needs to read this file, scan gmail date files, remove matching email lines, and delete the pending file. `FSStore.AppendPendingDelete` exists for the write side; the apply side is not yet implemented.

**Files affected:** `internal/store/fs.go`.

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

## Terminal `pigeon grep` silently drops GWS matches

`pigeon grep` in terminal mode parses rg's JSON output and tries to unmarshal
each match line into `modelv1.MsgLine` (`internal/search/parse.go`). GWS lines
(`EmailLine`, `EventLine`, `CommentLine`, `ReplyLine`) don't fit that type and
are silently dropped from the formatted summary, so GWS hits appear in the
match count but not in the grouped results.

Pipe mode (raw rg passthrough) already works — the issue is only the
terminal-formatted display.

## GWS Gmail: HTML-only emails lose the HTML body

`EmailLine.Text` is always populated, either from a multipart `text/plain`
part or from enmime's automatic HTML→text conversion when the message is
HTML-only. `EmailLine.HTML` is populated only when an explicit `text/html`
part exists inside a multipart message. Single-part HTML emails go through
enmime's auto-conversion and `HTML` is left empty — the raw HTML body is
discarded at parse time.

The read path (`pigeon read`, terminal grep display) never renders HTML in
either case — only `Text` is surfaced. So even when `EmailLine.HTML` is
present, it is dead storage.

The effect: emails with inline images, styled tables, or HTML-only
marketing templates are searchable and readable as flattened text, but
any information only expressible in markup (layout, linked images,
clickable links with different display text) is lost before it reaches
the user.

## `pigeon read` does not work for GWS accounts

`pigeon read --platform=gws --account=<acct> --contact=<q>` runs through
`findConversation`, which calls `store.ListConversations(acct)`. That walks
the account directory expecting messaging conversation subdirectories. For a
GWS account the top-level entries are `gmail`, `gcalendar`, `gdrive`,
`.sync-cursors.yaml`, etc. — service directories, not conversations. The
search matches service names (e.g. `--contact=gmail` matches the `gmail`
directory), then `store.ReadConversation` reads JSONL files under that path
and tries to decode each line into `modelv1.MsgLine`. GWS lines
(`EmailLine`, `EventLine`, `CommentLine`, `ReplyLine`) are not messaging
lines, so the read either returns empty results or produces nonsense.

There is no supported way to use `pigeon read` against GWS data. The
command does not error out explicitly — it silently succeeds with wrong
or missing output, which is worse than a clear rejection.

## `pigeon list` does not cover GWS accounts

`pigeon list` walks the messaging hierarchy (platforms → accounts →
conversations) from `store.Store`. GWS data has a different shape: per
account it has gmail / gcalendar / gdrive subtrees with different contents
(date files for gmail and calendar, per-file directories for drive). The
list command currently shows GWS accounts but stops there — no way to see
what's inside.

## Linear and GWS cursor load/save should share a single read/write path

`LoadLinearCursors`/`SaveLinearCursors` and `LoadGWSCursors`/`SaveGWSCursors` are near-identical: read YAML from `SyncCursorsPath()`, unmarshal into a typed struct, and the reverse for save. Each pair duplicates the file I/O, locking, directory creation, and error wrapping. There should be a single generic `loadCursors`/`saveCursors` helper that both call, so there's one function responsible for reading and one for writing the cursor file.

**Files affected:** `internal/store/fs.go`.

## `pigeon daemon status` does not list GWS accounts

`pigeon daemon status` calls the daemon's `GET /api/status` endpoint and
prints each platform's listeners under the `Listeners` map. The map is built
in `api.Server.handleStatus` by iterating `s.slack` and `s.whatsapp` — the
two sender maps on the `Server` struct. GWS pollers are never registered
with the API server (the struct has no `gws` field), so the status response
has no entry for GWS at all.

The effect: even when GWS pollers are running and actively producing data,
`pigeon daemon status` shows nothing for GWS. There is no way to tell from
`pigeon daemon status` whether a GWS account is running, stopped, or
crashed.

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
