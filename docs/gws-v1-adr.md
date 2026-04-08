# GWS V1 — Architecture Decision Record

## What was built

PR #94, merged as `b80d709`, adds poll-based sync for Gmail, Google
Drive (Docs + Sheets + Comments), and Google Calendar via the `gws`
CLI. Write-path only — data is polled, converted, and stored locally.
The read-path (search, display, `pigeon list/grep`) was deliberately
excluded; it will be designed separately as a unified abstraction
across messaging and GWS data.

## File inventory

```
internal/gws/
├── gws.go                          # CLI wrapper, APIError, IsCursorExpired
├── gws_test.go
├── model/
│   ├── email.go                    # EmailLine, EmailDeleteLine, EmailAttachment
│   ├── comment.go                  # CommentLine, ReplyLine
│   ├── event.go                    # EventLine (includes OriginalStartTime)
│   ├── meta.go                     # DocMeta, TabMeta
│   ├── doc.go                      # Document, Tab, Body, Paragraph, TextRun, etc.
│   ├── doc_test.go
│   ├── line.go                     # Line union type, Marshal, Parse
│   └── line_test.go
├── gwsstore/
│   ├── jsonl.go                    # AppendLine, ReadLines, Dedup (keep last by ID)
│   ├── jsonl_test.go
│   ├── content.go                  # WriteContent (replace-on-sync for .md/.csv)
│   ├── content_test.go
│   ├── cursors.go                  # Cursors (Gmail historyId, Drive pageToken, Calendar syncTokens)
│   ├── cursors_test.go
│   ├── meta.go                     # LoadMeta, SaveMeta
│   └── meta_test.go
├── gmail/
│   ├── client.go                   # GetHistoryID, ListHistory, GetMessage (format=raw)
│   ├── mime.go                     # parseRawMessage via enmime, parseAddress, parseAddresses
│   └── mime_test.go
├── calendar/
│   ├── client.go                   # ListEvents, SeedSyncToken, ToEventLine
│   └── client_test.go
├── drive/
│   ├── client.go                   # ListChanges, SeedPageToken, GetDocument, GetSheetNames,
│   │                               #   ReadSheetValues, ReadSheetFormulas, ListComments
│   └── converter/
│       ├── markdown.go             # MarkdownConverter.Convert → ConvertResult (markdown + images)
│       ├── markdown_test.go
│       ├── csv.go                  # ToCSV with row padding
│       └── csv_test.go
├── poller/
│   ├── poller.go                   # Poller (ticker loop, polls all 3 services)
│   ├── gmail.go                    # PollGmail (history → fetch → store JSONL)
│   ├── calendar.go                 # PollCalendar (syncToken → store JSONL)
│   ├── drive.go                    # PollDrive (changes → handleDoc/handleSheet)
│   ├── cursor_reset_test.go
│   ├── drive_test.go
│   └── validate_test.go           # Live smoke test (GWS_LIVE_TEST=1)

internal/daemon/gws_manager.go      # GWSManager lifecycle (start/stop/reconcile)
internal/config/config.go           # GWSConfig type, AddGWS method
internal/paths/gws.go               # Typed path hierarchy: GmailDir, CalendarDir, DriveDir, DriveFileDir
internal/paths/gws_test.go
docs/gws-protocol.md                # Full protocol spec
```

## Directory layout on disk

```
~/.local/share/pigeon/
└── gws/
    └── {account-slug}/                     # e.g. user-at-company-com
        ├── .sync-cursors.yaml              # all 3 cursors in one file
        ├── gmail/
        │   └── YYYY-MM-DD.jsonl
        ├── gdrive/
        │   ├── {title-slug}-{full-fileID}/
        │   │   ├── {TabName}.md
        │   │   ├── attachments/img-{objectId}.png
        │   │   ├── comments.jsonl
        │   │   └── meta.json
        │   └── {title-slug}-{full-fileID}/
        │       ├── {SheetName}.csv
        │       ├── {SheetName}.formulas.csv
        │       ├── comments.jsonl
        │       └── meta.json
        └── gcalendar/
            └── primary/
                └── YYYY-MM-DD.jsonl
```

## Design decisions

### Single `gws` platform, not three

Gmail, Drive, and Calendar are scoped under one `gws/{account-slug}/`
directory instead of three separate platform directories. One account =
one directory. Cursors are shared. Deleting an account cleans up one
place. This also prevents multi-account data collision — each account's
data is fully isolated under its own slug directory.

### Keep-last dedup, not keep-first

All JSONL types use "deduplicate by ID, keep last occurrence." For
immutable data (messages), duplicates are identical so order doesn't
matter. For mutable data (calendar events, comment resolved status),
keeping last ensures the latest state wins. This is a single rule
across the entire codebase — messaging data can adopt it too since
duplicate messages are identical.

### Write-path only, read-path deferred

The read-path (search/grep integration) was implemented, reviewed, and
then deliberately removed. The implementation added GWS content globs
(`*.md`, `*.csv`, `**/comments.jsonl`) to `read/grep.go` and a
`case 5` for 5-level GWS paths in `search/parse.go`. It was removed
because the read layer needs to be unified across all platforms — the
current read layer (`store.Store`, `modelv1.Line`) was designed for
conversational messaging data and GWS doesn't fit. Bolting on special
cases creates a diverging read path.

### enmime for Gmail MIME parsing

Gmail messages are fetched with `format=raw` (RFC 2822 bytes) and
parsed with `github.com/jhillyerd/enmime`. This replaced hand-rolled
MIME tree walking, base64url decoding, HTML tag stripping, and email
address parsing. enmime handles charset conversion, RFC 2047 encoded
headers, nested multipart structures, and attachment extraction.

### Text + HTML fields for email

`EmailLine.Text` is always populated — either from the `text/plain`
part or from enmime's automatic HTML→text conversion. `EmailLine.HTML`
is populated only when a multipart message has an explicit `text/html`
part. For single-part HTML emails, enmime converts to text and HTML is
omitted. This avoids blind spots (every email is greppable via `text`)
while preserving the raw HTML for future rendering.

### Inline image download via http.Get

The Docs API `contentUri` for inline images is a signed
`lh7-rt.googleusercontent.com` URL with a `?key=` parameter. It is
publicly accessible without auth headers. This was validated against a
real document — unauthenticated `http.Get` returned a PNG with HTTP
200. The URI is short-lived but the download happens immediately
during the poll cycle.

### Full file ID in drive slugs, no truncation

Drive directory names use `{title-slug}-{full-fileID}`. Truncating
the file ID was considered and rejected due to collision risk. The
full ~44-character file ID ensures uniqueness even when multiple
documents share the same title.

### Cursor expiry handling

Gmail returns HTTP 404 for expired `historyId`. Calendar returns HTTP
410 for expired `syncToken` or HTTP 400 with reason `"invalid"` for
corrupted tokens. `gws.IsCursorExpired(err)` checks all three cases.
When detected, the pollers clear the cursor and return nil — the next
poll cycle re-seeds automatically.

## Rate limits and scaling

### Per-user quotas

| API | Budget | Cost per poll call | Headroom at 20s interval |
|-----|--------|--------------------|--------------------------|
| Gmail `history.list` | 15,000 units/min | 2 units | 0.2% of budget |
| Gmail `messages.get` | 15,000 units/min | 5 units | ~25 units/min typical |
| Drive `changes.list` | 12,000 queries/min | 1 query | 0.03% of budget |
| Drive `files.export` | 12,000 queries/min | 1 query per changed file | burst on change |
| Drive `comments.list` | 12,000 queries/min | 1 query per changed file | burst on change |
| Calendar `events.list` | ~600 queries/min | 1 query | 0.17% of budget |

### Per-project daily quotas

| API | Daily limit | Polling cost/day (1 user, 20s) |
|-----|-------------|-------------------------------|
| Gmail | ~1,000,000,000 units | ~8,640 units (history.list only) |
| Drive | 1,000,000,000 queries | ~4,320 queries |
| Calendar | 1,000,000 queries | ~4,320 queries |

Calendar's daily limit is the binding constraint at scale. Polling
once per minute for 500 users = 720,000/day (within limit). Polling
every 20 seconds for 500 users = 2,160,000/day (over limit). Single
user at 20 seconds is negligible for all three services.

### Polling interval choice

20 seconds was chosen because:
- All three services stay under 1% of per-user rate limits.
- Drive and Calendar changes are infrequent enough that faster polling
  has diminishing returns.
- Gmail `history.list` is cheap (2 units). The expensive call is
  `messages.get` (5 units per message), which only fires when new
  messages arrive.
- Each poll cycle spawns 3 short-lived `gws` CLI processes (~30MB
  each, ~1 second lifetime). At 20-second intervals, the machine is
  idle 95% of the time.

### Backfill cost estimates (90 days, not yet implemented)

| Service | Items | API calls | Time | Storage |
|---------|-------|-----------|------|---------|
| Gmail | 5K-18K messages | 5K-18K `messages.get` | 2-6 min | 20-90MB |
| Calendar | 1K-3K events | 1-2 paginated calls | seconds | 0.5-1.5MB |
| Drive | 20-100 files | 50-300 calls | 30-60s | 1-20MB |

Gmail backfill is the heaviest — limited to 3,000 `messages.get`/min
by the 15,000 units/min per-user cap (5 units each). A heavy inbox
(18K messages) would take ~6 minutes.

Calendar backfill is nearly free — changing `SeedSyncToken` from
`timeMin=now` to `timeMin=now-90d` and `timeMax=now+90d` fetches all
events in the window AND returns the syncToken. One paginated call.

Drive backfill uses `files.list` with `modifiedTime > cutoff` to
enumerate recently modified docs/sheets, then the same per-file
export pipeline as incremental polling.

### Push notification alternative (researched, not used)

| Service | Push mechanism | Works locally? |
|---------|---------------|----------------|
| Gmail | Pub/Sub via `gws gmail +watch` | Yes (Pub/Sub pull) |
| Drive | Workspace Events API via `gws events +subscribe` | Yes (Pub/Sub pull) |
| Calendar | Webhook only (POST to public URL) | No |

Push was considered but polling was chosen for V1 because:
- Calendar has no Pub/Sub support, requiring polling regardless.
- Polling uses one pattern for all three services.
- No GCP project or Pub/Sub setup required.
- Gmail's Pub/Sub watch expires every 7 days and needs renewal.
- Drive's Workspace Events subscription expires in 7 days (without
  resource data) or 4 hours (with resource data).

## Rework areas

### Multi-account data isolation

Data paths were carefully scoped under `gws/{account-slug}/` to ensure
multiple Google accounts don't collide. The Poller struct accepts a
typed `paths.AccountDir` (not a raw string) to enforce this at the
type level. All path construction goes through the centralized
`internal/paths/gws.go` type hierarchy.

### Error propagation

Multiple instances of swallowed errors were identified and fixed during
review:
- `time.Parse` errors in `drive/client.go` comment/reply conversion
- Base64 decode errors in Gmail body extraction
- JSON unmarshal errors in `model/doc.go` list parsing
- Parse failures for malformed email From/To/CC headers

The pattern enforced: errors are always propagated or logged at
`slog.Error` level. `slog.Debug` is not used for real error cases.
Fallback values (empty string, nil) are returned alongside the error
log so the caller can distinguish "empty data" from "fetch failed."

### Library adoption over hand-rolled code

Three rounds of replacement occurred for Gmail MIME handling:
1. Initial: hand-rolled MIME tree walker, base64url decoder, regex
   HTML stripper, string-splitting address parser.
2. Intermediate: `net/mail.ParseAddress` for addresses,
   `golang.org/x/net/html` tokenizer for HTML stripping.
3. Final: `enmime.ReadEnvelope` replaced the entire pipeline.
   Gmail switched from `format=full` to `format=raw`.

The policy: prefer well-tested libraries over hand-rolled code for
solved problems (MIME parsing, HTML processing, email address parsing).

### Dead code removal

Code that was defined but never used in production was removed:
- Config fields not read by any code path
- Interface types with only one concrete implementation used directly
- Wrapper methods that only existed as test convenience

## Open items

Documented in `bugs.md` and `features.md`:

- No historical backfill on first run (bugs.md)
- Daemon has no restart/recovery for crashed account goroutines (bugs.md)
- Calendar recurring events not expanded (bugs.md)
- Drive comments re-fetched and duplicated on every poll (bugs.md)
- No maintenance/compaction for GWS JSONL files (bugs.md)
- `pigeon setup-gws` command needed (features.md)
- Read-path integration deferred (design decision above)

## External dependencies added

- `github.com/jhillyerd/enmime` v1.3.0 — MIME parsing for Gmail
  `format=raw` messages. Handles charset conversion, RFC 2047, nested
  multipart, attachments.

## Test coverage

All tests pass on `go test ./...`.

The live smoke test (`TestLiveSmoke` in `poller/validate_test.go`)
requires `GWS_LIVE_TEST=1` and authenticated `gws` CLI. It seeds all
three cursors, creates a test Google Doc, polls Drive, verifies the
markdown and meta.json land on disk, then cleans up the test doc.

## Prerequisite: gws CLI

The entire GWS integration shells out to the `gws` CLI for all Google
Workspace API calls. The CLI must be installed and authenticated before
the daemon can poll. Auth is handled by `gws` (OAuth via keyring) —
pigeon does not store or manage Google credentials.
