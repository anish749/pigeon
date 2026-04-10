# Bugs / Tech Debt

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

## Maintain() detects thread files by path substring

`FSStore.Maintain()` distinguishes thread files from date files using `strings.Contains(path, "/threads/")`. This is fragile — if a conversation name ever contained "threads" as a path segment (e.g. a Slack channel named `threads`), the path would be `…/threads/2026-04-06.txt` and a date file inside it would be misidentified as a thread file and compacted with `CompactThread` instead of `Compact`.

**Files affected:** `internal/store/storev1/fs.go` (`Maintain`).

## Implement maintenance in the daemon

This needs to run per account because the in case of WhatsApp the daemon may hold a setup lock, like the db lock is n needed.
So just think about the case when maintenance is running and there's a separate setup WhatsApp command running.
This means like only one of the things, either the daemon or the one of the clients needs to first gain the lock on that account and then they can work.

## Slack mentions are not being delivered to Claude

When someone mentions inside a thread uh or I don't know if it in case of other other states what happens, but plot didn't work.

## Send reactions to Claude Code sessions

## Dedup messages by last message id

## GWS: no historical backfill on first run

GWS pollers seed their cursors to "now" and only capture future changes. Unlike Slack (which backfills 90 days of history on first run), a fresh GWS setup starts with an empty view — existing emails, docs, sheets, and calendar events are not synced.

**Files affected:** `internal/gws/poller/gmail.go`, `internal/gws/poller/calendar.go`, `internal/gws/poller/drive.go`, `internal/gws/calendar/client.go`.

## Daemon has no restart/recovery for crashed accounts

All per-account goroutines (Slack listeners, WhatsApp listeners, GWS pollers) are fire-and-forget. If one crashes or returns an error, it logs and the goroutine exits permanently. Nothing restarts it — the account is dead until the entire daemon is restarted.

This affects all platforms equally:

- **Slack**: `smClient.RunContext` and `listener.Run` run in goroutines. If either exits, the account stops receiving messages.
- **WhatsApp**: same pattern — goroutine exits on error, no recovery.
- **GWS**: `p.Run(child)` exits on error, poller is gone.

The Slack socket mode library handles network-level reconnection internally, but if the goroutine itself panics or the library gives up, pigeon doesn't recover.

**Files affected:** `internal/daemon/slack_manager.go`, `internal/daemon/whatsapp_manager.go`, `internal/daemon/gws_manager.go`.

## GWS Calendar: recurring events not expanded

Sync tokens require `singleEvents=false` (the default), which returns parent recurring events with RRULEs instead of expanded individual instances. The `EventLine` model has no RRULE field, so a weekly standup is stored as a single event with just the first occurrence's start date. All future instances are invisible.

This means: if someone has a recurring "Team Standup" every Monday, pigeon stores one event for the series but has no way to know it occurs every Monday. Querying "what meetings do I have next Tuesday" would miss it.

Related edge cases that also need handling: modified instances (single occurrence rescheduled), cancelled instances (single occurrence deleted), and series cancellation (all instances removed).

**Files affected:** `internal/gws/model/event.go`, `internal/gws/calendar/client.go`, `internal/gws/poller/calendar.go`.

## GWS Drive: comments re-fetched and duplicated on every poll

`storeComments` fetches ALL comments for a file on every Drive change, not just new ones. Each comment and reply is appended to `comments.jsonl` unconditionally. If a doc has 200 comments and someone fixes a typo, all 200 comments are appended again as duplicates.

Dedup-on-read (keep last by ID) means the data is still correct, but the file grows unboundedly — every edit multiplies the comment count in the JSONL. A doc with 200 comments edited 50 times produces 10,000 lines where 200 are unique.

**Files affected:** `internal/gws/poller/drive.go` (`storeComments`), `internal/gws/drive/client.go` (`ListComments`).

## Slack send does not resolve mentions

When Pigeon sends a Slack message, @mentions are not resolved — they appear as raw text instead of being converted to Slack's mention format. Recipients see plain-text "@name" that doesn't ping or link to the mentioned user.

## GWS Drive: removed files not cleaned up from disk

When a Drive file is deleted or trashed, `changes.list` reports it with `removed=true`. The poller logs this at debug level and skips the file, but the local directory (`gdrive/{slug}-{fileID}/`) with its `.md`, `.csv`, `comments.jsonl`, and `meta.json` remains on disk indefinitely.

During backfill, `files.list` uses `trashed=false` so trashed files are never fetched. But files that were backfilled and later deleted will leave stale directories behind.

## GWS: no maintenance/compaction for JSONL files

The protocol spec describes dedup and compaction for GWS JSONL files (emails, comments, calendar events), and `gwsstore.Dedup` exists for read-time dedup. But the existing maintenance pass (`FSStore.Maintain`) only handles messaging data (`modelv1` lines). GWS JSONL files are never compacted.

Combined with the comments re-fetch bug above, this means `comments.jsonl` files grow without bound. Gmail and calendar JSONL files also accumulate duplicate event updates that are never cleaned up.

**Files affected:** `internal/gws/gwsstore/jsonl.go`, `internal/gws/poller/`.

## GWS Gmail: email deletes should use a pending-deletes file

The poller currently appends `email-delete` tombstone lines to today's date file when `history.list` reports a deletion. This is broken: the tombstone and the original email are in different date files, and `Dedup` (which would reconcile them) is never called from the read path. Deleted emails remain visible.

The poller should not try to locate and rewrite the original date file at poll time — it doesn't know the email's date, and scanning all date files during a sync event is too expensive.

Instead: the poller should write deleted message IDs to a separate pending-deletes file (e.g. `.pending-email-deletes` in the gmail dir). Maintenance is then responsible for reading the pending-deletes file, finding the corresponding email lines across date files, removing them, and clearing the pending-deletes file.

**Files affected:** `internal/gws/poller/gmail.go`, `internal/gws/gwsstore/`, `internal/gws/model/email.go`.

## Validate date where calendar events are attributed

Right now we used a start date. This can be particularly problematic for multi day events.
We need to validate how exactly this works in practice.


## Setup commands need a revamp

The three setup commands (`setup-whatsapp`, `setup-slack`, `setup-gws`) have
diverged in shape and UX, and the root help text is out of date now that GWS
is a first-class platform.

Observable issues:

- **`pigeon` root help omits GWS.** `internal/cli/root.go`'s `Long` description
  walks through WhatsApp and Slack under `WORKFLOW — FIRST-TIME SETUP`, and the
  example `config.yaml` in the `CONFIG` section shows only `whatsapp:` and
  `slack:` blocks. `setup-gws` is listed in the Setup group but never
  documented alongside the others.
- **Prompt libraries are inconsistent.** `setup-slack` uses `bufio.NewReader`
  with hand-rolled `fmt.Print` prompts, `setup-whatsapp` drives its own
  interactive flow around QR pairing, `setup-gws` uses `promptui`. Three
  setup commands, three prompt styles.
- **Output shapes diverge.** Each command has its own header banner
  ("Slack Workspace Setup\n======"), its own confirmation footer, and its
  own tone. There is no shared scaffolding for "detect state → prompt → save
  → tell the user what to do next."
- **Auth models are very different but that difference isn't surfaced.**
  `setup-slack` runs an OAuth server in-process. `setup-whatsapp` pairs a
  device via QR. `setup-gws` is a thin config writer because `gws` owns auth
  externally. The help text doesn't prepare users for any of this, and the
  commands themselves don't explain where auth lives relative to pigeon.

**Files affected:** `internal/cli/root.go`, `internal/cli/setup.go`,
`internal/commands/setup_slack.go`, `internal/commands/setup_whatsapp.go`,
`internal/commands/setup_gws.go`.

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

## Separate out ConvMeta
// MetaFile returns the path to the conversation's .meta.json sidecar.
func (c ConversationDir) MetaFile() MetaFile {
	return MetaFile(filepath.Join(c.Path(), ".meta.json"))
}
Rename the type here to be ConvMetaFile.
use a constant for the .meta.json file name.
