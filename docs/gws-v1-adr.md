# GWS V1 — Architecture Decision Record

## What was built

PR #94, merged as `b80d709`, adds poll-based sync for Gmail, Google
Drive (Docs + Sheets + Comments), and Google Calendar via the `gws`
CLI. Write-path only — data is polled, converted, and stored locally.
The read-path (search, display, `pigeon list/grep`) was deliberately
excluded; it will be designed separately as a unified abstraction
across messaging and GWS data.

3,871 lines of Go across `internal/gws/`, plus daemon integration,
config, path types, and a protocol spec.

## File inventory

```
internal/gws/
├── gws.go                          # CLI wrapper, APIError, IsCursorExpired
├── gws_test.go                     # 5 tests for error helpers
├── model/
│   ├── email.go                    # EmailLine, EmailDeleteLine, EmailAttachment
│   ├── comment.go                  # CommentLine, ReplyLine
│   ├── event.go                    # EventLine (includes OriginalStartTime)
│   ├── meta.go                     # DocMeta, TabMeta
│   ├── doc.go                      # Document, Tab, Body, Paragraph, TextRun, etc.
│   ├── doc_test.go                 # AllTabs flattening, InlineObjects, Lists parsing
│   ├── line.go                     # Line union type, Marshal, Parse
│   └── line_test.go                # Round-trip for all 5 line types
├── gwsstore/
│   ├── jsonl.go                    # AppendLine, ReadLines, Dedup (keep last by ID)
│   ├── jsonl_test.go               # Dedup, delete semantics, corrupt line handling
│   ├── content.go                  # WriteContent (replace-on-sync for .md/.csv)
│   ├── content_test.go
│   ├── cursors.go                  # Cursors (Gmail historyId, Drive pageToken, Calendar syncTokens)
│   ├── cursors_test.go
│   ├── meta.go                     # LoadMeta, SaveMeta
│   └── meta_test.go
├── gmail/
│   ├── client.go                   # GetHistoryID, ListHistory, GetMessage (format=raw)
│   ├── mime.go                     # parseRawMessage via enmime, parseAddress, parseAddresses
│   └── mime_test.go                # 8 tests: plain, multipart, HTML-only, attachments, RFC 2047
├── calendar/
│   ├── client.go                   # ListEvents, SeedSyncToken, ToEventLine
│   └── client_test.go             # 3 tests: timed, all-day, recurring
├── drive/
│   ├── client.go                   # ListChanges, SeedPageToken, GetDocument, GetSheetNames,
│   │                               #   ReadSheetValues, ReadSheetFormulas, ListComments
│   └── converter/
│       ├── markdown.go             # MarkdownConverter.Convert → ConvertResult (markdown + images)
│       ├── markdown_test.go        # 5 tests: headings, formatting, lists, tables, links
│       ├── csv.go                  # ToCSV with row padding
│       └── csv_test.go             # 4 tests: uniform, ragged, empty, nil
├── poller/
│   ├── poller.go                   # Poller (ticker loop, polls all 3 services)
│   ├── gmail.go                    # PollGmail (history → fetch → store JSONL)
│   ├── calendar.go                 # PollCalendar (syncToken → store JSONL)
│   ├── drive.go                    # PollDrive (changes → handleDoc/handleSheet)
│   ├── cursor_reset_test.go        # 2 tests for IsCursorExpired detection
│   ├── drive_test.go               # driveSlug test
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
directory instead of three separate platform directories (`gmail/`,
`gdrive/`, `gcalendar/`). One account = one directory. Cursors are
shared. Deleting an account cleans up one place. This was chosen after
a multi-account collision bug was found — the pollers originally wrote
to unscoped root paths, so two accounts would overwrite each other.

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

### Docs markdown converter from gws_utils

The markdown converter was copied from `cb/gws_utils` (a separate repo
at `/Users/anish/cb/gws_utils`). It walks the Google Docs API JSON
response (StructuralElement → Paragraph → TextRun) and produces
markdown. It handles headings, bold, italic, strikethrough, links,
ordered/unordered lists, tables, and inline images.

### Inline image download via http.Get

The Docs API `contentUri` for inline images is a signed
`lh7-rt.googleusercontent.com` URL with a `?key=` parameter. It is
publicly accessible without auth headers. This was validated against a
real document — unauthenticated `http.Get` returned a 90KB PNG with
HTTP 200. The URI is short-lived but the download happens immediately
during the poll cycle.

### Full file ID in drive slugs, no truncation

Drive directory names use `{title-slug}-{full-fileID}` (e.g.
`project-roadmap-1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms`).
An earlier implementation truncated to 8 characters, which created
collision risk. The full 44-character file ID is used because
uniqueness matters more than directory name length.

### Cursor expiry handling

Gmail returns HTTP 404 for expired `historyId`. Calendar returns HTTP
410 for expired `syncToken` or HTTP 400 with reason `"invalid"` for
corrupted tokens. `gws.IsCursorExpired(err)` checks all three cases.
When detected, the pollers clear the cursor and return nil — the next
poll cycle re-seeds automatically.

### 20-second poll interval

All three services poll every 20 seconds. Rate limit analysis showed
this uses less than 1% of per-user quota for all three APIs. Gmail
(15,000 units/min, 2 units per `history.list`), Drive (12,000
queries/min), Calendar (~600 queries/min).

## Rework history

### Multi-account data collision (critical)

The pollers originally wrote to unscoped paths like
`gmail/YYYY-MM-DD.jsonl`. Two GWS accounts would silently overwrite
each other. Fixed by scoping all data under `gws/{account-slug}/`.
The Poller struct was changed from `accountDir string` to
`account paths.AccountDir`.

### Fixes applied to wrong branch

All review fixes (error propagation, storeComments helper, accountDir
scoping) were initially committed to `gws/integration` (the last PR in
the stack). They belonged to earlier branches (`gws/calendar`,
`gws/gmail`, `gws/drive`). The fix commits were reverted from
`gws/integration`, then applied to the correct branches, and the
entire stack was rebased. This required resolving merge conflicts at
every rebase step.

### Swallowed errors (multiple instances)

- `toCommentLine` / `toReplyLine` in `drive/client.go` used
  `ts, _ := time.Parse(...)`. Fixed to return `(T, error)`.
- `findBody` in `gmail/mime.go` returned `""` on base64 decode failure.
  Fixed to return `(string, error)` and propagate through `ExtractBody`.
- `parseLists` in `model/doc.go` returned `nil` on unmarshal failure.
  Fixed to return `(map, error)` and propagate through `AllTabs`.

### Dead code shipped

- `GWSConfig.Services []string` field was defined but never read by any
  code. Removed.
- `Convert(tab) string` wrapper on `MarkdownConverter` existed
  alongside `ConvertWithImages(tab) ConvertResult`. Only the tests
  called `Convert`. Removed the wrapper, renamed `ConvertWithImages`
  to `Convert`, updated tests to use `.Markdown` field.
- `Converter` interface was defined but never used as an interface
  (only `MarkdownConverter` existed, always used concretely). Removed.

### Hand-rolled code replaced with libraries

- Email address parsing (`parseFrom`, `parseAddressList`) was
  hand-rolled with string splitting. Replaced with `net/mail`, then
  replaced again with `enmime.ParseAddressList`.
- HTML tag stripping was a regex `<[^>]*>`. Replaced with
  `golang.org/x/net/html` tokenizer, then replaced again when enmime
  was adopted (enmime handles HTML→text internally).
- The entire Gmail MIME parsing pipeline (tree walking, base64url
  decoding, header extraction, attachment collection) was hand-rolled.
  Replaced with `enmime.ReadEnvelope` using `format=raw`.

### Search magic number

`fileIncludes` in `commands/search.go` used `len(includes) == 4` to
detect "no date files matched." Adding a glob would break the check.
Fixed to use a `dateFiles` counter.

### ModifiedTime was time.Now()

`DocMeta.ModifiedTime` was set to the local clock instead of the
actual Drive file modification time. Fixed to use
`ch.File.ModifiedTime` from the Drive changes API response (also
required adding `modifiedTime` to the `fields` query parameter).

### Error logs hidden as debug

`parseFrom` and `parseAddressList` fallbacks initially logged at
`slog.Debug`. Corrected to `slog.Error` — parse failures are real
errors that should be visible.

### eventDate silent fallback

Cancelled events with no parseable start date were filed to
`unknown.jsonl` with no log output. Added `slog.Warn` with event ID
and status.

### Duplicate comment storage blocks

`handleDoc` and `handleSheet` had ~15 identical lines for fetching and
appending comments. Extracted to `storeComments(fileDir, fileID)`.

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
- `golang.org/x/net/html` — moved from indirect to direct dependency
  (was already in go.mod via other deps). Used briefly for HTML
  stripping before enmime adoption; no longer directly imported but
  remains as transitive dependency.

## Test coverage

51 test functions across 14 test files. All pass on `go test ./...`.

The live smoke test (`TestLiveSmoke` in `poller/validate_test.go`)
requires `GWS_LIVE_TEST=1` and authenticated `gws` CLI. It seeds all
three cursors, creates a test Google Doc, polls Drive, verifies the
markdown and meta.json land on disk, then cleans up the test doc.

## Prerequisite: gws CLI

The entire GWS integration shells out to the `gws` CLI for all Google
Workspace API calls. The CLI must be installed and authenticated before
the daemon can poll. Auth is handled by `gws` (OAuth via keyring) —
pigeon does not store or manage Google credentials.
