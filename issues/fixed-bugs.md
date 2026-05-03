# Fixed Bugs

## ~~Dedup messages by last message id~~ — won't fix

Duplicates on disk are expected and by design. Both the real-time listener and sync can write the same message, but maintenance deduplicates by message ID when it runs. The window between duplicate write and maintenance is intentional — date files are append-only logs, not point-in-time-accurate views. Readers already go through compaction/resolution which deduplicates in memory.

## ~~Maintain() detects thread files by path substring~~ — fixed in #153

Thread file detection now uses `paths.IsThreadFile` which checks filename format (date vs timestamp), not parent directory name. A conversation literally named `threads` is no longer misidentified.

## ~~Separate out ConvMeta~~ — fixed in #151

`MetaFile` type renamed to `ConvMetaFile`. `.meta.json` string literal replaced with `ConvMetaFilename` constant.

## ~~GWS Drive: removed files not cleaned up from disk~~ — fixed in #154

`FSStore.RemoveDriveFile` scans the gdrive directory by fileID and deletes matching local directories when Drive reports a file as removed.

## ~~GWS: no historical backfill on first run~~ — fixed in #135, #136, #137

Gmail, Calendar, and Drive pollers now backfill on first run.

## ~~GWS Calendar: recurring events not expanded~~ — fixed in #135

Calendar backfill expands recurring events.

## ~~GWS Gmail: email-delete tombstones~~ — fixed in #159

Tombstone (`email-delete`) lines replaced with a pending-deletes file. The poller writes deleted IDs to `.pending-email-deletes`; maintenance is responsible for applying them. `EmailDeleteLine` type removed.

## ~~GWS Drive: removed files not cleaned up~~ — rejected approach in #152, fixed in #154

#152 was rejected because it put filesystem IO (`os.ReadDir`, `os.RemoveAll`) in the `paths` package and had the poller doing storage bookkeeping directly. #154 moved the logic to `FSStore.RemoveDriveFile` in the store layer.

## ~~GWS Gmail: hard delete at poll time~~ — rejected in #156

Rejected: rewriting date files during sync is too expensive — the poller doesn't know the email's date, so it would scan all date files on every deletion. The correct design is to write deleted IDs to a pending-deletes file during sync and let maintenance handle the actual removal (#159).

## ~~Root help omits GWS~~ — fixed in #163

Added GWS to config example, data layout tree, JSON fields, setup workflow, and grep examples in `pigeon help`.

## ~~Slack: edits and deletes skipped when user_id is empty~~ — fixed in #215

Sender resolution for edit and delete handlers now uses three-way lookup (user ID, bot ID, username), matching the approach for new messages. Bot-authored edits/deletes are no longer silently dropped.

## ~~Gmail: keyring backend stdout pollution causes poll failure~~ — fixed in #217

GWS CLI handler now captures stdout and stderr separately. Error parsing tries stdout first where the gws CLI writes structured errors.

## ~~Slack: send fails to MPDM channels with channel_not_found~~ — fixed in #230

Bot sends to MPDMs are now rejected early with actionable guidance to use `--via pigeon-as-user` instead of failing after outbox review.

## ~~`pigeon read` does not work for GWS accounts~~ — fixed in #198

`pigeon read` now rejects GWS and Linear platforms with a clear error instead of silently returning wrong output. GWS read semantics tracked as a feature.

## ~~Slack: @Slackbot DM fetch fails on every sync~~ — fixed in 06d6b7f

USLACKBOT is now filtered out before fetch, preventing the `channel_not_found` warning on every sync cycle.

## ~~Slack: "all messages filtered" after sync fetch~~ — fixed in #228, #229

Unified filter now checks blocks, attachments, and files (not just text) so bot/integration messages with non-text content are no longer incorrectly filtered. Store layer also persists blocks and attachments.

## ~~Hub: reaction notifications lack context about the reacted message~~ — fixed in #265

Reaction notifications now include the original message content. The hub looks up the reacted-to message via `read.Grep` (searching both date files and thread files) and formats it using `FormatReactionNotification`. Falls back to `FormatReactionFallbackNotification` when the message is not on disk.

## ~~Terminal UI review does not allow changing the send mode~~ — fixed in #252

The outbox review screen now supports toggling send mode with the `v` key before approving the message.

## ~~Terminal UI review does not show the recipient/target name~~ — fixed in #257

`itemSummary` now displays `platform → resolved target (from sender): message` using `ResolvedTarget()` which resolves Slack channel IDs and user IDs to display names at submit time.

## ~~Validate date where calendar events are attributed~~ — fixed in #261

Multi-day events are now expanded to all spanned date files instead of only the start date.

## ~~Implement maintenance in the daemon~~ — done

The daemon now runs `FSStore.Maintain` per configured account through a single-worker queue (`MaintenanceManager`). Two trigger sources push into the same channel: an hourly scheduler (enqueues accounts whose `.maintenance.json` mtime is missing or ≥24h old by wall clock — survives laptop suspend, unlike a monotonic ticker) and an explicit `Trigger(ctx, acct)` API used by the slack listener after each sync. The single consumer guarantees only one Maintain pass is in flight across the whole daemon, so eager and periodic compaction can never race on the same files. `Trigger` is a context-aware blocking send: backpressure flows back to the trigger source if the queue fills, and shutdown unblocks parked senders. The original WhatsApp setup-lock concern does not apply — Maintain only walks JSONL log files, not the WhatsApp SQLite DB, so the CLI setup command and the daemon's maintenance loop don't compete for the same lock.
