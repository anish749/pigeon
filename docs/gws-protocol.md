# Google Workspace Storage Protocol

Pigeon stores Google Workspace data as plain text files, greppable with
standard tools. This protocol covers four data types: Gmail messages,
Google Docs, Google Sheets, and Google Calendar events. All files are
UTF-8 encoded.

This document describes the on-disk wire format, directory layout, and
sync behaviour. It is a contract for what lands on disk, not a code
reference — anything a reader or external tool needs to understand to
work with pigeon's Google Workspace storage should be here.

## Storage Philosophy

Per-item JSONL collections (calendar events, drive comments) are stored
as the **raw API response** plus a single injected `type` discriminator.
Pigeon does not pick a subset of "interesting" fields — the full
response from the Google API lands on disk verbatim. This makes storage
lossless against any field we don't currently use and future-proof
against new fields Google adds. The unit of grep for these collections
is the line, which represents one whole item.

Parsed-content files (Docs → markdown, Sheets → CSV) are stored in a
**converted form** because the raw API response isn't greppable — text
is fragmented across styled runs, interleaved with metadata. For these,
the converted output IS the greppable form.

Envelope-and-body data (Gmail) uses parsed fields on the line because
the actual message content lives inside base64-encoded MIME, not in the
JSON envelope. Parsing MIME is necessary for greppability.

## Deduplication Rule

All JSONL line types use the same rule: **deduplicate by ID, keep last
occurrence.** This applies uniformly to emails, comments, and calendar
events. For immutable data (messages), duplicates are identical so first
vs last produces the same result. For mutable data (events, comments
with resolved status changes), keeping last ensures the latest state
wins.

## Polling and Sync

All four data types use poll-based sync via the `gws` CLI. Each service
has a cursor that tracks the last-seen state. The daemon polls every 20
seconds, calling gws with the saved cursor and processing the delta.

| Service | Cursor type | gws command |
|---------|-------------|-------------|
| Gmail | `historyId` (monotonic integer) | `gmail users history list` |
| Drive | `pageToken` (opaque string) | `drive changes list` |
| Calendar | `syncToken` + expanded window state | `calendar events list` |

### First-Run Backfill

On first sync (no cursor) each service backfills ~90 days of history
before acquiring the cursor for incremental polling:

| Service | Backfill method |
|---------|-----------------|
| Gmail | `gmail messages list` with `q=after:{now-90d}`, then fetch each |
| Drive | `drive files list` with `modifiedTime > now-90d` filter, then process each file through the same pipeline as incremental changes |
| Calendar | `calendar events list` with `timeMin=now-90d`, expanding each recurring event via `events.instances` within ±90 days |

Backfill writes data to disk before the cursor is saved. If the backfill
is interrupted, re-running starts over — idempotency comes from the
keep-last-by-ID dedup rule.

### Sync Procedure

1. Load cursor from `.sync-cursors.yaml`.
2. If no cursor, run the service's backfill, then seed the cursor.
3. Call the service with the cursor. Paginate until the final page
   (the one that returns the next cursor, not a `nextPageToken`).
4. Process each item in the delta.
5. Save the new cursor.

### Cursor Expiry

| Service | Cursor validity | Recovery |
|---------|----------------|----------|
| Gmail | `historyId` valid ~1 week | Full resync (re-seed + backfill) |
| Drive | `pageToken` does not expire | N/A |
| Calendar | `syncToken` valid indefinitely; occasionally invalidated by server | Full resync (re-seed + backfill) |

When a cursor is rejected (HTTP 410 Gone for Calendar, HTTP 404 for
Gmail, or HTTP 400/invalid for Calendar), the poller clears the cursor
and re-seeds on the next cycle.

## Directory Layout

```
~/.local/share/pigeon/
└── gws/                                        # platform
    ├── {account-slug}/                         # e.g. user-at-company-com
    │   ├── .sync-cursors.yaml                  # all cursors (gmail, drive, calendar)
    │   ├── .poll-metrics.jsonl                 # per-poll telemetry
    │   ├── gmail/
    │   │   └── YYYY-MM-DD.jsonl                # email messages
    │   ├── gdrive/
    │   │   ├── {doc-title-slug-fileId}/        # Google Doc
    │   │   │   ├── {TabName}.md                # one markdown file per tab
    │   │   │   ├── attachments/                # inline images
    │   │   │   │   └── img-{objectId}.png      # downloaded image file
    │   │   │   ├── comments.jsonl              # comment threads
    │   │   │   └── drive-meta-YYYY-MM-DD.json  # sync metadata, dated by modifiedTime
    │   │   └── {sheet-title-slug-fileId}/      # Google Sheet
    │   │       ├── {SheetName}.csv             # cell values per sheet
    │   │       ├── {SheetName}.formulas.csv    # formulas per sheet (optional)
    │   │       ├── comments.jsonl              # comment threads
    │   │       └── drive-meta-YYYY-MM-DD.json  # sync metadata
    │   └── gcalendar/
    │       └── {calendar-slug}/                # "primary", etc.
    │           └── YYYY-MM-DD.jsonl            # events by start date
    └── {another-account-slug}/
        └── ...
```

### Multiple Accounts

Each Google account is scoped under `gws/{account-slug}/` with
independent cursors. All services (Gmail, Drive, Calendar) for one
account share a single directory and cursor file. The poller iterates
over all configured accounts on each cycle.

A single user might have:
- `gws/personal-at-email-com/` and `gws/work-at-company-com/`

Each account authenticates independently via gws. Account configuration
lives in pigeon's config file alongside Slack and WhatsApp accounts.

### Account Slugs

The account slug is derived from the Google account email address using
the same `gosimple/slug` rules as other platforms. Example:
`user@company.com` → `user-at-company-com`.

### Cursor File

All three services share a single `.sync-cursors.yaml` per account
at `gws/{account-slug}/.sync-cursors.yaml`:

```yaml
gmail:
  history_id: "12975259"
drive:
  page_token: "1339021"
calendar:
  primary:
    sync_token: "CP_frMzs2JMDEP_frMzs2JMDGAUg..."
    expanded_until: "2026-07-07T22:05:12Z"
    recurring_events:
      - "ql2e63sqv9o8jb46grq4msqijc"
      - "e27b9a1sjhrn69l7bfatv4ultc"
```

The calendar cursor is structured rather than a plain token because the
calendar syncer tracks three pieces of state per calendar: the Google
`syncToken` for incremental delta, the end of the window into which
recurring events have been expanded (`expanded_until`), and the list of
known recurring event parent IDs so window expansion can extend them
without refetching the change list.

### Poll Metrics

`.poll-metrics.jsonl` records one line per poll cycle with per-service
durations, item counts, and error flags. Used for monitoring and
debugging. Append-only; rotated on disk by size.

---

## Gmail

### Platform

`gmail`

### Organization

Gmail messages are organized by **date only** — no conversation
directories. Labels and thread IDs are fields on each message line.
This keeps the directory structure flat and avoids the multi-label
problem (a message can belong to INBOX, IMPORTANT, and CATEGORY_UPDATES
simultaneously).

### Sync Flow

1. **First run:** backfill via `gmail messages list` with
   `q=after:{now-90d}`. Fetch each returned message, parse, and write.
   Then acquire the starting `historyId` from `gmail users getProfile`.
2. **Incremental:** poll `gmail users history list` with `startHistoryId`.
3. Response contains `messagesAdded` and `messagesDeleted` arrays.
4. For each added message, fetch the full message via
   `gmail users messages get` with `format=raw`.
5. Decode the base64url RFC 2822 body, extract headers, body parts, and
   attachment metadata. Store as one JSONL line.
6. For each deleted message, append its ID to `.pending-email-deletes`.
   Maintenance applies the deletes by scanning date files.

### Line Types

Unlike calendar events and drive comments, Gmail line types are
**parsed**, not raw. The Gmail API response wraps the actual message
content in a base64-encoded RFC 2822 MIME envelope, which is not
directly greppable — so pigeon parses the MIME and stores extracted
fields (from, subject, body text, attachment metadata) on the line.
The raw MIME bytes are not currently persisted.

#### Email Line

```json
{"type":"email","id":"19d644c4ddc7c70c","threadId":"19d644c4ddc7c70c","ts":"2026-04-06T10:15:02Z","from":"alice@example.com","fromName":"Alice","to":["bob@example.com"],"cc":["charlie@example.com"],"subject":"Q2 planning","labels":["INBOX","IMPORTANT"],"snippet":"Hey team, let's discuss...","text":"Hey team, let's discuss the Q2 roadmap.\n\nBest,\nAlice","attach":[{"id":"part-1","type":"application/pdf","name":"roadmap.pdf"}]}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"email"` | Line type discriminator |
| `id` | string | Gmail message ID |
| `threadId` | string | Gmail thread ID (groups related messages) |
| `ts` | RFC 3339 | Message timestamp (from `internalDate`) |
| `from` | string | Sender email address |
| `fromName` | string | Sender display name (from `From` header) |
| `to` | []string | Recipient email addresses |
| `cc` | []string | CC addresses (omitted if empty) |
| `subject` | string | Email subject line |
| `labels` | []string | Gmail label IDs (`INBOX`, `SENT`, `IMPORTANT`, etc.) |
| `snippet` | string | Gmail's text preview (HTML-decoded) |
| `text` | string | Plain text body (from `text/plain` part, or enmime's HTML→text conversion) |
| `html` | string | Raw HTML body (only present for multipart emails with a `text/html` part; omitted otherwise) |
| `attach` | []object | Attachments: `{id, type, name}` |

### Pending Deletes

Path: `gws/{account}/gmail/.pending-email-deletes`

One message ID per line. Written by the poller when `history.list`
reports deletions. The poller does not know which date file contains the
email, so actual removal is deferred to maintenance. Maintenance reads
all pending IDs, scans date files, removes matching email lines, and
deletes the pending file.

### Date File

Path: `gws/{account}/gmail/YYYY-MM-DD.jsonl`

Messages are filed by their `internalDate` (when Gmail received them).
One file per day. Append-only.

### Body Extraction

Messages are fetched with `format=raw`, returning the full RFC 2822
bytes (base64url-encoded). The `enmime` library parses the MIME
envelope, handling charset conversion, RFC 2047 encoded headers,
and nested multipart structures.

- `text` is always populated: either from the `text/plain` part, or
  from enmime's automatic HTML→text conversion for HTML-only emails.
- `html` is populated only when the message has an explicit
  `text/html` part in a multipart message. For single-part HTML
  emails, enmime converts to text and `html` is omitted.

### Attachments

Attachment metadata is stored on the email line. The actual file content
is **not downloaded** in V1 — only the metadata (name, MIME type, part
ID) is preserved. The attachment can be fetched on demand via
`gmail users messages attachments get` using the stored message ID and
attachment ID.

### Thread Display

On read, messages with the same `threadId` are grouped for display.
The subject line of the first message in the thread is the thread title.
Thread grouping is a display-time operation, not a storage concern.

### Deduplication

Messages are deduplicated by `id` (keep last occurrence). Deleted
messages are removed from date files during maintenance (see Pending
Deletes above).

---

## Google Docs

### Platform

`gdrive`

### Organization

Each Google Doc is a directory named by the slugified document title
plus the Drive file ID (for collision resistance when two docs share a
title). The directory contains:

- `{TabName}.md` — one markdown file per document tab.
- `attachments/` — inline images referenced by the markdown.
- `comments.jsonl` — comment threads as JSONL (one comment per line,
  replies nested inside).
- `drive-meta-YYYY-MM-DD.json` — sync metadata, filename encodes the
  file's `modifiedTime` date for efficient "find recently modified
  files" queries.

### Document Tabs

Google Docs support multiple tabs within a single document. Each tab
is exported as a separate markdown file named by the tab title. For
single-tab documents (the common case), this produces one file named
after the default tab (e.g. `Tab 1.md`).

The `documents.get` API returns a `tabs` array. Each tab has
`tabProperties.title` and `tabProperties.tabId`. The poller exports
each tab independently via `docs documents.get` with
`includeTabsContent=true`, then converts each tab's body to markdown.

### Sync Flow

1. **First run:** backfill via `drive files list` with
   `modifiedTime > now-90d` and `mimeType` filter. Each returned file
   flows through the same pipeline as an incremental change.
2. **Incremental:** poll `drive changes list` with `pageToken`.
3. For each changed file where `mimeType` is
   `application/vnd.google-apps.document`:
   a. Fetch the document: `docs documents get` with
      `includeTabsContent=true`.
   b. For each tab, convert the body to markdown. Store as
      `{TabName}.md`. If a tab is renamed, the old file remains
      and a new file is created.
   c. Fetch comments: `drive comments list`.
      Write `comments.jsonl` as a full snapshot (see Comment Sync below).
4. The document directory name is the slugified title plus the Drive
   file ID. If the title changes, a new directory is created — the old
   directory remains with its last-synced content.

### Content Files

Path: `gws/{account}/gdrive/{doc-slug}/{TabName}.md`

Each tab's body exported as markdown. **Replaced** on each sync (not
appended). Directly greppable — plain markdown, not JSONL.

The markdown export preserves:
- Headings, bold, italic, links, lists, tables.
- Inline images as `![alt](attachments/img-{objectId}.png)` references.
  The image files are downloaded to the `attachments/` subdirectory.

It does **not** preserve:
- Comments (stored separately in `comments.jsonl`).
- Suggestions/tracked changes.
- Drawing objects.
- Smart chips, person mentions.

### Comment Line Format

Each line is the **raw Drive comment JSON** (everything the API
returned) plus a `"type":"comment"` discriminator. Replies are nested
inside their parent comment's `replies` array, exactly as the Drive API
returns them. Only the `type` key is injected by pigeon; every other
field is verbatim from the API response.

```json
{"type":"comment","id":"AAABedbmvS8","kind":"drive#comment","author":{"kind":"drive#user","displayName":"User A","me":false},"content":"416 may return a body...","htmlContent":"<p>416 may return a body...</p>","createdTime":"2025-04-04T15:42:13Z","modifiedTime":"2025-04-04T15:42:13Z","resolved":false,"quotedFileContent":{"mimeType":"text/html","value":"416"},"replies":[{"id":"reply-xyz","kind":"drive#reply","author":{"displayName":"User B"},"content":"You can do this with the current design...","createdTime":"2025-04-04T16:00:00Z","modifiedTime":"2025-04-04T16:00:00Z","action":"reopen"}]}
```

Fields callers commonly rely on (the rest are preserved but may or may
not be interesting):

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"comment"` | Storage discriminator (injected, not from API) |
| `id` | string | Drive comment ID |
| `author.displayName` | string | Comment author |
| `content` | string | Comment text (plain) |
| `htmlContent` | string | Comment text as HTML |
| `createdTime` | RFC 3339 | When the comment was created |
| `modifiedTime` | RFC 3339 | When the comment was last edited |
| `resolved` | bool | Whether the thread is resolved |
| `quotedFileContent.value` | string | Highlighted/quoted text the comment anchors to |
| `replies[]` | array | Nested replies; each reply has its own `id`, `author`, `content`, `createdTime`, `modifiedTime`, optional `action` (`"resolve"`/`"reopen"`) |

### Comment Sync

Comments use **full-snapshot overwrite**, not append. The Drive comments
API has no incremental sync cursor — every call returns the full set of
comments for a file. On each sync that touches the file:

1. Fetch all comments with `drive comments list` (paginate).
2. Write the entire `comments.jsonl` file, replacing the previous
   contents atomically.

This is simpler than append + dedup and cleans up deleted comments
automatically (they're just not in the new snapshot). Resolved-status
changes and content edits also flow through without special handling.

Deduplication on read: still by `id` (keep last), though in practice
every line in a freshly-written `comments.jsonl` has a unique ID.

### Metadata File

Path: `gws/{account}/gdrive/{doc-slug}/drive-meta-YYYY-MM-DD.json`

The filename encodes the file's `modifiedTime` as `YYYY-MM-DD`. This
lets the read layer find recently modified files by filename glob
without opening each meta file. A given directory has exactly one
meta file at a time; when `modifiedTime` changes, the new meta file
replaces the old one (old name is deleted on write).

```json
{
  "fileId": "1abc...",
  "mimeType": "application/vnd.google-apps.document",
  "title": "Project Roadmap",
  "modifiedTime": "2026-03-15T14:22:00Z",
  "syncedAt": "2026-03-16T09:00:00Z",
  "tabs": [{"id": "t.0", "title": "Tab 1"}, {"id": "t.1", "title": "Notes"}]
}
```

| Field | Description |
|-------|-------------|
| `fileId` | Google Drive file ID (stable identifier) |
| `mimeType` | `application/vnd.google-apps.document` or `application/vnd.google-apps.spreadsheet` |
| `title` | Original document title (before slugification) |
| `modifiedTime` | Last modified time from Drive metadata |
| `syncedAt` | When pigeon last synced this document |
| `tabs` | (Docs only) Tab ID and title for each document tab |
| `sheets` | (Sheets only) Names of the sheet tabs |

Used for incremental sync: skip re-export if `modifiedTime` hasn't
changed since the last synced version on disk.

Unlike the per-item JSONL collections, meta files are hand-shaped with
a small set of fields. They are not meant to be grepped across; they
answer targeted lookups ("when was this file last synced?", "what's the
title?") per directory.

---

## Google Sheets

### Platform

`gdrive` (same as Docs — both are Drive files)

### Organization

Each Google Sheet is a directory named by the slugified spreadsheet
title plus the Drive file ID. The directory contains:

- `{SheetName}.csv` — cell values for each sheet tab.
- `{SheetName}.formulas.csv` — formulas for each sheet tab (optional).
- `comments.jsonl` — comment threads as JSONL (same format as Docs).
- `drive-meta-YYYY-MM-DD.json` — sync metadata.

### Sync Flow

1. **First run:** same backfill as Docs — `drive files list` with the
   spreadsheet mimeType also included.
2. **Incremental:** poll `drive changes list` with `pageToken`.
3. For each changed file where `mimeType` is
   `application/vnd.google-apps.spreadsheet`:
   a. Fetch sheet metadata: `sheets spreadsheets get` with
      `fields=sheets.properties` to discover sheet names.
   b. For each sheet: `sheets values get` with `valueRenderOption=FORMATTED_VALUE`.
      Convert the `values` 2D array to CSV. Store as `{SheetName}.csv`.
   c. Optionally fetch formulas: `sheets values get` with
      `valueRenderOption=FORMULA`. Store as `{SheetName}.formulas.csv`.
   d. Fetch comments: same as Google Docs.

### Value Files

Path: `gws/{account}/gdrive/{sheet-slug}/{SheetName}.csv`

Standard CSV format. **Replaced** on each sync. Directly greppable.

The Sheets API returns a 2D array of strings. Empty cells are empty
strings. Trailing empty cells may be omitted from a row. The CSV writer
pads rows to uniform width.

### Formula Files

Path: `gws/{account}/gdrive/{sheet-slug}/{SheetName}.formulas.csv`

Same format as value files, but cells that contain formulas show the
formula (e.g. `=SUM(A1:A10)`) instead of the computed value. Cells
without formulas show their value. This is the Sheets API's
`FORMULA` render option behavior.

Formula files are optional — they are only created when the poller is
configured to capture formulas. They add one additional API call per
sheet per sync cycle.

### Comments

Same JSONL format and sync procedure as Google Docs. Comments on
spreadsheets use the same Drive comments API. The `anchor` field
references the cell or range the comment is attached to.

### Metadata File

Same path convention and schema as Google Docs, with `mimeType` set to
`application/vnd.google-apps.spreadsheet`. Includes the list of sheet
names instead of tabs:

```json
{
  "fileId": "1abc...",
  "mimeType": "application/vnd.google-apps.spreadsheet",
  "title": "Budget",
  "modifiedTime": "2026-03-15T14:22:00Z",
  "syncedAt": "2026-03-16T09:00:00Z",
  "sheets": ["Summary", "Revenue", "Expenses"]
}
```

---

## Google Calendar

### Platform

`gcalendar`

### Organization

Calendar events are organized by **calendar** (the "conversation"
equivalent) and **start date**. The calendar directory name is the
calendar ID slug (`primary` → `primary`, a shared calendar ID gets
slugified).

### Sync Flow

Calendar sync runs as three phases per poll cycle:

1. **Seed (first run only):**
   a. `events.list` with `timeMin=now-90d` and `singleEvents=false`
      returns all events in the window, including parent recurring
      events with their RRULEs.
   b. For each parent recurring event, `events.instances` expands
      instances within ±90 days.
   c. All one-off events, exception instances, and expanded recurring
      instances are written to disk.
   d. The sync token, `expanded_until` marker, and list of recurring
      event IDs are saved to the cursor.

2. **Incremental:** `events.list` with the saved `syncToken` returns
   changed events. Re-expand any recurring parents that changed (their
   instances are overwritten on disk by the keep-last dedup). Track new
   recurring events; remove cancelled recurring events from the cursor.

3. **Window expansion:** if `expanded_until` is within 30 days of now,
   re-expand all tracked recurring events out to now+90d and update
   `expanded_until`.

### Line Format

Each line is the **raw Google Calendar event JSON** (everything the API
returned) plus a `"type":"event"` discriminator. Only the `type` key is
injected by pigeon; every other field is verbatim from the Calendar API.

```json
{"type":"event","kind":"calendar#event","id":"1ukte1f8011837nnv5kjnfnnag","status":"confirmed","created":"2026-04-06T12:23:11.000Z","updated":"2026-04-06T12:23:12.345Z","summary":"Q2 Planning","description":"Discuss roadmap priorities","location":"Room 4B","creator":{"email":"alice@example.com","displayName":"Alice"},"organizer":{"email":"alice@example.com","displayName":"Alice","self":true},"start":{"dateTime":"2026-04-07T10:00:00+02:00","timeZone":"Europe/Berlin"},"end":{"dateTime":"2026-04-07T11:00:00+02:00","timeZone":"Europe/Berlin"},"iCalUID":"1ukte...@google.com","sequence":0,"attendees":[{"email":"bob@example.com","displayName":"Bob","responseStatus":"accepted"},{"email":"charlie@example.com","responseStatus":"needsAction"}],"hangoutLink":"https://meet.google.com/abc-defg-hij","conferenceData":{"entryPoints":[{"entryPointType":"video","uri":"https://meet.google.com/abc-defg-hij","label":"meet.google.com/abc-defg-hij"}],"conferenceId":"abc-defg-hij"},"eventType":"default"}
```

Fields callers commonly rely on (the rest are preserved but may or may
not be interesting — see the [Google Calendar Events resource](https://developers.google.com/calendar/api/v3/reference/events#resource)
for the full list):

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"event"` | Storage discriminator (injected, not from API) |
| `id` | string | Google Calendar event ID |
| `status` | string | `"confirmed"`, `"tentative"`, or `"cancelled"` |
| `summary` | string | Event title |
| `start.dateTime` / `start.date` | RFC 3339 / `YYYY-MM-DD` | Start (timed events use `dateTime`, all-day use `date`) |
| `end.dateTime` / `end.date` | RFC 3339 / `YYYY-MM-DD` | End |
| `created` / `updated` | RFC 3339 | Creation and last modification times |
| `recurringEventId` | string | Present on instances of a recurring event |
| `recurrence` | []string | Present on parent recurring events (RRULE strings); parents are not written to disk — only their instances are |
| `originalStartTime` | `EventDateTime` | Present on cancelled recurring instances in place of `start` |
| `hangoutLink` | string | Google Meet link (omitted if none) |

All-day events use `start.date`/`end.date` as `YYYY-MM-DD` strings
without a `dateTime` field. Timed events use `start.dateTime`/`end.dateTime`
as RFC 3339 with timezone offset and optionally a `timeZone` field.

### Date File

Path: `gws/{account}/gcalendar/{calendar-slug}/YYYY-MM-DD.jsonl`

Events are filed by their **start date**. Timed events use the date
portion of `start.dateTime`. All-day events use `start.date`. Cancelled
recurring instances (which lack `start`) use `originalStartTime`.

### Event Updates

When an event is updated, the new version is appended to the date file
(same file if the start date didn't change, new file if it did). The
reader deduplicates by event `id`, keeping the **last occurrence**.
Maintenance compacts duplicate event IDs down to the most recent
version.

### Cancelled Events

When `events.list` returns an event with `status: "cancelled"`, the
event line is appended with that status. On read, cancelled events are
excluded from display. Maintenance removes cancelled events and any
prior versions of the same ID.

### Recurring Events

The Calendar API offers two ways to get recurring event data:
`singleEvents=true` (server expands instances) and `singleEvents=false`
(server returns parents + RRULEs). Pigeon uses `singleEvents=false`
with client-side expansion via `events.instances`, for two reasons:

1. `singleEvents=true` without a `timeMax` expands decades into the
   future (observed: 12,750+ events paginating into 2049 on a calendar
   with ~40 recurring events).
2. `singleEvents=true` *with* `timeMax` scopes the sync token to the
   time window, creating a permanent blind spot for instances beyond
   the original window.

`singleEvents=false` gives a calendar-wide sync token with no time
scoping, and `events.instances` provides bounded expansion per parent
event. The cursor tracks `expanded_until` so window extension is
incremental.

Parent recurring events (those with a `recurrence` array) are **not
written to disk** — only their expanded instances are. Each instance
carries a `recurringEventId` pointing back to its parent.

---

## Greppability

### JSONL Files (Gmail, Comments, Calendar)

Standard text tools work directly:

```bash
# Find emails from alice
grep '"from":"alice@' ~/.local/share/pigeon/gws/*/gmail/

# Find calendar events mentioning "planning"
grep -ri 'planning' ~/.local/share/pigeon/gws/*/gcalendar/

# Find events where Bob's response is "needsAction"
grep '"email":"bob@example.com","responseStatus":"needsAction"' ~/.local/share/pigeon/gws/*/gcalendar/

# Find resolved comments
grep '"resolved":true' ~/.local/share/pigeon/gws/*/gdrive/*/comments.jsonl

# Find comment threads that were resolved (reply with action:"resolve")
grep '"action":"resolve"' ~/.local/share/pigeon/gws/*/gdrive/*/comments.jsonl

# Find emails with PDF attachments
grep 'application/pdf' ~/.local/share/pigeon/gws/*/gmail/

# Count emails per day
wc -l ~/.local/share/pigeon/gws/*/gmail/2026-04-*.jsonl
```

Because calendar events and comments are stored as raw API JSON, any
field the Google API returns is grep-able. Adding a new query doesn't
require a code change — `jq` or `grep` on the stored lines is enough.

### Content Files (Docs as Markdown, Sheets as CSV)

Plain text — directly greppable without any JSONL parsing:

```bash
# Search across all Google Docs
grep -r 'algorithm' ~/.local/share/pigeon/gws/*/gdrive/*/*.md

# Search across all Sheet data
grep -r 'revenue' ~/.local/share/pigeon/gws/*/gdrive/*/*.csv

# Search formulas
grep '=SUM' ~/.local/share/pigeon/gws/*/gdrive/*/*.formulas.csv

# Search everything (docs, sheets, comments, emails, events)
rg 'quarterly' ~/.local/share/pigeon/gws/
```

### Pigeon Search Integration

The `pigeon grep` command uses ripgrep with `--json` for structured
parsing. GWS content files (`.md`, `.csv`, `comments.jsonl`) are
included in search globs alongside `.jsonl` date files.

---

## Read Protocol

### Gmail

1. Parse all email lines from the requested date range.
2. Deduplicate by `id` (keep last occurrence).
3. Sort by timestamp.
4. Optionally group by `threadId` for threaded display.

### Google Docs

1. Read `{TabName}.md` files and display as markdown.
2. Parse `comments.jsonl`, deduplicate by `id` (keep last occurrence).
   Each line is a raw comment with its replies already nested inside.
3. Display comments grouped by anchor text; replies are in the
   comment's `replies` array.
4. Resolved comments can be shown or hidden based on user preference.

### Google Sheets

1. Read `{SheetName}.csv` files and display as tables.
2. Comments same as Google Docs.

### Calendar

1. Parse event lines from the requested date range.
2. Deduplicate by `id` (keep **last** occurrence — latest state wins).
3. Exclude cancelled events.
4. Sort by start time.

---

## Maintenance

### Gmail

1. Apply pending deletes (scan date files, remove matching email
   lines, delete the `.pending-email-deletes` file).
2. Deduplicate by `id` (keep last), sort by timestamp, rewrite.

### Google Docs / Sheets

- `*.md` and `*.csv` are replaced on each sync — no maintenance
  needed for content files.
- `comments.jsonl`: rewritten as a full snapshot on each sync that
  touches the file — no maintenance pass needed.

### Calendar

Deduplicate by `id` (keep last occurrence), remove cancelled events
and any prior versions of the same `id`, sort by start time, rewrite.

---

## Known Limitations

### Gmail

- **Attachment content is not downloaded.** Only metadata (name, type,
  part ID) is stored. Fetching the actual file requires an additional
  API call with the stored IDs.
- **HTML-only emails get converted text.** enmime auto-converts HTML
  to plain text. The `text` field is always searchable, but for
  single-part HTML emails the raw HTML is not preserved (only the
  converted text). Multipart emails preserve both `text` and `html`.
- **Draft and label changes are not tracked.** The history API reports
  `labelAdded`/`labelRemoved` events, but V1 only tracks
  `messageAdded`/`messageDeleted`.

### Google Docs

- **No suggestion/tracked-change support.** Only the current accepted
  text is exported. Pending suggestions are not stored.
- **Markdown export is lossy.** Drawings, embedded objects, and some
  formatting may not survive the markdown conversion.
- **Comment author has no email.** The Drive comments API returns
  `displayName` but not the author's email address.

### Google Sheets

- **CSV export loses formatting.** Cell colors, fonts, borders, and
  conditional formatting are not captured.
- **Multi-sheet comments are not per-sheet.** The Drive comments API
  returns all comments for the file, not organized by sheet tab.
- **Large sheets may exceed API limits.** The Sheets API returns at
  most 10 million cells per request.

### Calendar

- **Event moves change the date file.** If an event's start time
  changes to a different date, the old version remains in the old
  date file and the new version is appended to the new date file.
  Maintenance handles this by removing stale entries (same `id`,
  older `updated` timestamp).
- **Recurring event instances are time-bounded.** Only instances
  within the expansion window (±90 days by default) are on disk.
  Instances outside the window are refetched as the window slides
  forward via Phase 3 (window expansion).
- **All-day events span multiple days.** A 3-day all-day event is
  filed under its `start.date` only, not under each day it spans.
