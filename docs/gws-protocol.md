# Google Workspace Storage Protocol

Pigeon stores Google Workspace data as plain text files, greppable with
standard tools. This protocol covers four data types: Gmail messages,
Google Docs, Google Sheets, and Google Calendar events. All files are
UTF-8 encoded.

## Deduplication Rule

All JSONL line types use the same rule: **deduplicate by ID, keep last
occurrence.** This applies uniformly to emails, comments, replies, and
calendar events. For immutable data (messages), duplicates are identical
so first vs last produces the same result. For mutable data (events,
comment resolved status), keeping last ensures the latest state wins.

## Polling and Sync

All four data types use poll-based sync via the `gws` CLI. Each service
has a cursor that tracks the last-seen state. The daemon polls every 20
seconds, calling gws with the saved cursor and processing the delta.

| Service | Cursor type | gws command | Seed command |
|---------|-------------|-------------|--------------|
| Gmail | `historyId` (monotonic integer) | `gmail users history list` | `gmail users getProfile` |
| Drive | `pageToken` (opaque string) | `drive changes list` | `drive changes getStartPageToken` |
| Calendar | `syncToken` (opaque string) | `calendar events list` | `calendar events list` with `timeMin=now` |

### Sync Procedure

1. Load cursor from `.sync-cursors.yaml`.
2. If no cursor, seed it (first run).
3. Call the service with the cursor. Paginate until the final page
   (the one that returns the next cursor, not a `nextPageToken`).
4. Process each item in the delta.
5. Save the new cursor.

### Cursor Expiry

| Service | Cursor validity | Recovery |
|---------|----------------|----------|
| Gmail | `historyId` valid ~1 week | Full resync (re-seed from profile) |
| Drive | `pageToken` does not expire | N/A |
| Calendar | `syncToken` valid indefinitely (invalidated by server rarely) | Full resync (re-seed with `timeMin`) |

When a cursor is rejected (HTTP 410 Gone for Calendar, HTTP 404 for
Gmail), the poller clears the cursor and re-seeds on the next cycle.

## Directory Layout

```
~/.local/share/pigeon/
└── gws/                                        # platform
    ├── {account-slug}/                         # e.g. user-at-company-com
    │   ├── .sync-cursors.yaml                  # all cursors (gmail, drive, calendar)
    │   ├── gmail/
    │   │   └── YYYY-MM-DD.jsonl                # email messages
    │   ├── gdrive/
    │   │   ├── {doc-title-slug}/               # Google Doc
    │   │   │   ├── {TabName}.md                # one markdown file per tab
    │   │   │   ├── attachments/                # inline images
    │   │   │   │   └── img-{objectId}.png      # downloaded image file
    │   │   │   ├── comments.jsonl              # comment threads
    │   │   │   └── meta.json                   # sync metadata
    │   │   └── {sheet-title-slug}/             # Google Sheet
    │   │       ├── {SheetName}.csv             # cell values per sheet
    │   │       ├── {SheetName}.formulas.csv    # formulas per sheet (optional)
    │   │       ├── comments.jsonl              # comment threads
    │   │       └── meta.json                   # sync metadata
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
  primary: "CP_frMzs2JMDEP_frMzs2JMDGAUg..."
  work: "CJ2abc..."
```

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

1. Poll `gmail users history list` with `startHistoryId`.
2. Response contains `messagesAdded` and `messagesDeleted` arrays.
3. For each added message, fetch the full message via
   `gmail users messages get` with `format=full`.
4. Extract headers, body, and attachments. Store as one JSONL line.
5. For each deleted message, append a delete line.

### Line Types

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
| `text` | string | Plain text body (`text/plain` part) |
| `attach` | []object | Attachments: `{id, type, name}` |

#### Email Delete Line

```json
{"type":"email-delete","id":"19d644c4ddc7c70c","ts":"2026-04-06T12:00:00Z"}
```

Appended when `history.list` reports a message was deleted (trashed/removed).

### Date File

Path: `gws/{account}/gmail/YYYY-MM-DD.jsonl`

Messages are filed by their `internalDate` (when Gmail received them).
One file per day. Append-only.

### Body Extraction

The Gmail API returns a MIME tree. The body is extracted by walking the
tree depth-first:

1. Find the first `text/plain` part → `text` field.
2. If no `text/plain`, find the first `text/html` part and strip tags.
3. Body data is base64url-encoded in the API response — decode before
   storing.
4. Parts with `attachmentId` (instead of `data`) are attachment
   references, not inline content.

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

Messages are deduplicated by `id` (keep last occurrence). Delete lines
cause the target message to be excluded on read. Maintenance applies
deletes and removes both lines.

---

## Google Docs

### Platform

`gdrive`

### Organization

Each Google Doc is a directory named by the slugified document title.
The directory contains:

- `{TabName}.md` — one markdown file per document tab.
- `comments.jsonl` — comment threads as JSONL.
- `.meta.json` — sync metadata.

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

1. Poll `drive changes list` with `pageToken`.
2. For each changed file where `mimeType` is
   `application/vnd.google-apps.document`:
   a. Fetch the document: `docs documents get` with
      `includeTabsContent=true`.
   b. For each tab, convert the body to markdown. Store as
      `{TabName}.md`. If a tab is renamed, the old file remains
      and a new file is created.
   c. Fetch comments: `drive comments list` with all fields.
      Append new comments and replies to `comments.jsonl`.
3. The document directory name is the slugified title from the file
   metadata. If the title changes, a new directory is created (same
   limitation as channel renames in the messaging protocol).

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

### Comment Line Types

#### Comment Line

```json
{"type":"comment","id":"AAABedbmvS8","ts":"2025-04-04T15:42:13Z","author":"User A","content":"416 may return a body...","anchor":"416","resolved":false}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"comment"` | Line type discriminator |
| `id` | string | Drive comment ID |
| `ts` | RFC 3339 | `createdTime` from the API |
| `author` | string | `author.displayName` |
| `content` | string | Comment text |
| `anchor` | string | Quoted/highlighted text the comment is attached to (`quotedFileContent.value`, HTML-decoded) |
| `resolved` | bool | Whether the comment thread is resolved |

#### Reply Line

```json
{"type":"reply","id":"reply-xyz","commentId":"AAABedbmvS8","ts":"2025-04-04T16:00:00Z","author":"User B","content":"You can do this with the current design...","action":"reopen"}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"reply"` | Line type discriminator |
| `id` | string | Drive reply ID |
| `commentId` | string | Parent comment ID |
| `ts` | RFC 3339 | `createdTime` |
| `author` | string | `author.displayName` |
| `content` | string | Reply text |
| `action` | string | `"resolve"` or `"reopen"` if this reply changed the comment's resolved state. Omitted for normal replies. |

### Comment Sync

Comments are **append-only**. On each sync:

1. Fetch all comments with `drive comments list` (paginate).
2. For each comment not already in `comments.jsonl` (by ID), append it.
3. For each reply not already stored (by ID), append it.
4. If a comment's `resolved` status changed, append an updated comment
   line. The reader uses the latest line for a given comment ID.

Deduplication on read: by `id` for comments, by `id` for replies. Keep
the last occurrence (latest state wins).

### Metadata File

Path: `gws/{account}/gdrive/{doc-slug}/.meta.json`

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
| `mimeType` | Always `application/vnd.google-apps.document` for docs |
| `title` | Original document title (before slugification) |
| `modifiedTime` | Last modified time from Drive metadata |
| `syncedAt` | When pigeon last synced this document |
| `tabs` | Tab ID and title for each document tab |

Used for incremental sync: skip re-export if `modifiedTime` hasn't
changed since `syncedAt`.

---

## Google Sheets

### Platform

`gdrive` (same as Docs — both are Drive files)

### Organization

Each Google Sheet is a directory named by the slugified spreadsheet
title. The directory contains:

- `{SheetName}.csv` — cell values for each sheet tab.
- `{SheetName}.formulas.csv` — formulas for each sheet tab (optional).
- `comments.jsonl` — comment threads as JSONL (same format as Docs).
- `.meta.json` — sync metadata.

### Sync Flow

1. Poll `drive changes list` with `pageToken`.
2. For each changed file where `mimeType` is
   `application/vnd.google-apps.spreadsheet`:
   a. Fetch sheet metadata: `sheets spreadsheets get` with
      `fields=sheets.properties` to discover sheet names.
   b. For each sheet: `sheets +read --spreadsheet ID --range SheetName`.
      Convert the `values` 2D array to CSV. Store as `{SheetName}.csv`.
   c. Optionally fetch formulas: Sheets API `values.get` with
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

Same schema as Google Docs, with `mimeType` set to
`application/vnd.google-apps.spreadsheet`. Additionally includes
the list of sheet names for efficient re-sync:

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

1. Poll `calendar events list` with `syncToken` for each calendar.
2. Response contains changed events (added, updated, or cancelled).
3. For each event, append or update in the appropriate date file.
4. Cancelled events are stored as event lines with `status: "cancelled"`.

### Line Type

#### Event Line

```json
{"type":"event","id":"1ukte1f8011837nnv5kjnfnnag","ts":"2026-04-06T12:23:11Z","updated":"2026-04-06T12:23:12Z","status":"confirmed","summary":"Q2 Planning","description":"Discuss roadmap priorities","start":"2026-04-07T10:00:00+02:00","end":"2026-04-07T11:00:00+02:00","startDate":"","endDate":"","location":"Room 4B","organizer":"alice@example.com","attendees":["bob@example.com","charlie@example.com"],"meetLink":"https://meet.google.com/abc-defg-hij","eventType":"default","recurring":false}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"event"` | Line type discriminator |
| `id` | string | Google Calendar event ID |
| `ts` | RFC 3339 | Event creation time |
| `updated` | RFC 3339 | Last modification time |
| `status` | string | `"confirmed"`, `"tentative"`, or `"cancelled"` |
| `summary` | string | Event title |
| `description` | string | Event description/notes |
| `start` | RFC 3339 | Start datetime (timed events) |
| `end` | RFC 3339 | End datetime (timed events) |
| `startDate` | `YYYY-MM-DD` | Start date (all-day events, mutually exclusive with `start`) |
| `endDate` | `YYYY-MM-DD` | End date (all-day events) |
| `location` | string | Event location |
| `organizer` | string | Organizer email |
| `attendees` | []string | Attendee emails |
| `meetLink` | string | Google Meet link (omitted if none) |
| `eventType` | string | `"default"`, `"focusTime"`, `"outOfOffice"`, `"workingLocation"` |
| `recurring` | bool | Whether this is an instance of a recurring event |

### Date File

Path: `gws/{account}/gcalendar/{calendar-slug}/YYYY-MM-DD.jsonl`

Events are filed by their **start date**. Timed events use the local
date from the `start.dateTime` field. All-day events use `start.date`.

### Event Updates

When an event is updated, the new version is **appended** to the date
file (same file if the start date didn't change, new file if it did).
The reader deduplicates by event `id`, keeping the **last occurrence**
(latest state wins). Maintenance compacts duplicate event IDs down to
the most recent version.

All JSONL types across pigeon use the same dedup rule: keep last
occurrence by ID. This works uniformly for immutable data (duplicate
messages are identical, so first vs last doesn't matter) and mutable
data (calendar events, comment resolved status) where the latest
version is the truth.

### Cancelled Events

When `events.list` returns an event with `status: "cancelled"`, the
event line is appended with that status. On read, cancelled events are
excluded from display (similar to delete lines in the messaging
protocol). Maintenance removes cancelled events and any prior versions.

### Recurring Events

The poller uses `syncToken`-based incremental sync, which defaults to
`singleEvents=false`. This means the API returns recurring event
templates (with recurrence rules) and individual instance exceptions
(modified or cancelled occurrences). The `recurring` field is set to
`true` when `recurringEventId` is present in the API response.

Cancelled recurring instances carry `originalStartTime` instead of
`start`/`end`. The `eventDate` function uses this field for date
filing.

---

## Greppability

### JSONL Files (Gmail, Comments, Calendar)

Standard text tools work directly:

```bash
# Find emails from alice
grep '"from":"alice@' ~/.local/share/pigeon/gws/*/gmail/

# Find all calendar events mentioning "planning"
grep -ri 'planning' ~/.local/share/pigeon/gws/*/gcalendar/

# Find resolved comments
grep '"resolved":true' ~/.local/share/pigeon/gws/*/gdrive/*/comments.jsonl

# Find replies that resolved a comment thread
grep '"action":"resolve"' ~/.local/share/pigeon/gws/*/gdrive/*/comments.jsonl

# Find emails with PDF attachments
grep 'application/pdf' ~/.local/share/pigeon/gws/*/gmail/

# Count emails per day
wc -l ~/.local/share/pigeon/gws/*/gmail/2026-04-*.jsonl
```

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
3. Apply deletes (exclude emails with a matching `email-delete` line).
4. Sort by timestamp.
5. Optionally group by `threadId` for threaded display.

### Google Docs

1. Read `{TabName}.md` files and display as markdown.
2. Parse `comments.jsonl`, deduplicate by `id` (keep last occurrence).
3. Display comments grouped by anchor text, with replies nested.
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

Same as messaging: deduplicate by `id`, apply deletes, sort by
timestamp, rewrite.

### Google Docs / Sheets

- `*.md` and `*.csv` are replaced on each sync — no maintenance
  needed for content files.
- `comments.jsonl`: deduplicate comments by `id` (keep last), deduplicate
  replies by `id` (keep last), sort by timestamp, rewrite.

### Calendar

Deduplicate by `id` (keep last occurrence), remove cancelled events
and any prior versions of the same `id`, sort by start time, rewrite.

---

## Data Model

```go
// Gmail

type EmailLine struct {
    ID        string       `json:"id"`                  // Gmail message ID
    ThreadID  string       `json:"threadId"`            // Gmail thread ID
    Ts        time.Time    `json:"ts"`                  // when Gmail received it
    From      string       `json:"from"`                // sender email
    FromName  string       `json:"fromName"`            // sender display name
    To        []string     `json:"to"`                  // recipient emails
    CC        []string     `json:"cc,omitempty"`        // CC emails
    Subject   string       `json:"subject"`             // email subject
    Labels    []string     `json:"labels"`              // Gmail labels
    Snippet   string       `json:"snippet"`             // text preview
    Text      string       `json:"text"`                // plain text body
    Attach    []EmailAttachment `json:"attach,omitempty"` // attachments
}

type EmailAttachment struct {
    ID   string `json:"id"`   // Gmail attachment/part ID
    Type string `json:"type"` // MIME type
    Name string `json:"name"` // filename
}

type EmailDeleteLine struct {
    ID string    `json:"id"` // target message ID
    Ts time.Time `json:"ts"` // when the delete was observed
}

// Drive Comments (shared by Docs and Sheets)

type CommentLine struct {
    ID       string    `json:"id"`                // Drive comment ID
    Ts       time.Time `json:"ts"`                // created time
    Author   string    `json:"author"`            // display name
    Content  string    `json:"content"`           // comment text
    Anchor   string    `json:"anchor,omitempty"`  // highlighted text
    Resolved bool      `json:"resolved"`          // thread resolved state
}

type ReplyLine struct {
    ID        string    `json:"id"`                // Drive reply ID
    CommentID string    `json:"commentId"`         // parent comment ID
    Ts        time.Time `json:"ts"`                // created time
    Author    string    `json:"author"`            // display name
    Content   string    `json:"content"`           // reply text
    Action    string    `json:"action,omitempty"`  // "resolve" or "reopen"
}

// Drive Metadata

type DocMeta struct {
    FileID       string    `json:"fileId"`
    MimeType     string    `json:"mimeType"`
    Title        string    `json:"title"`
    ModifiedTime string    `json:"modifiedTime"`
    SyncedAt     string    `json:"syncedAt"`
    Tabs         []TabMeta `json:"tabs,omitempty"`   // doc tabs (Docs only)
    Sheets       []string  `json:"sheets,omitempty"` // sheet names (Sheets only)
}

type TabMeta struct {
    ID    string `json:"id"`    // tab ID (e.g. "t.0")
    Title string `json:"title"` // tab title (e.g. "Tab 1")
}

// Calendar

type EventLine struct {
    ID          string   `json:"id"`                    // Calendar event ID
    Ts          string   `json:"ts"`                    // created time
    Updated     string   `json:"updated"`               // last modified time
    Status      string   `json:"status"`                // confirmed, tentative, cancelled
    Summary     string   `json:"summary"`               // event title
    Description string   `json:"description,omitempty"` // event notes
    Start       string   `json:"start,omitempty"`       // RFC 3339 datetime (timed)
    End         string   `json:"end,omitempty"`         // RFC 3339 datetime (timed)
    StartDate   string   `json:"startDate,omitempty"`   // YYYY-MM-DD (all-day)
    EndDate     string   `json:"endDate,omitempty"`     // YYYY-MM-DD (all-day)
    Location    string   `json:"location,omitempty"`    // event location
    Organizer   string   `json:"organizer,omitempty"`   // organizer email
    Attendees   []string `json:"attendees,omitempty"`   // attendee emails
    MeetLink    string   `json:"meetLink,omitempty"`    // Google Meet link
    EventType   string   `json:"eventType"`             // default, focusTime, etc.
    Recurring   bool     `json:"recurring,omitempty"`   // instance of recurring event
}
```

## Known Limitations

### Gmail

- **Attachment content is not downloaded.** Only metadata (name, type,
  part ID) is stored. Fetching the actual file requires an additional
  API call with the stored IDs.
- **HTML-only emails lose formatting.** If an email has no `text/plain`
  part, the HTML is stripped to plain text. Rich formatting is lost.
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
- **Recurring event expansion.** Sync with `syncToken` returns
  individual instance changes, not the full recurrence rule. If a
  recurring event's rule changes, all affected instances are returned
  as individual updates.
- **All-day events span multiple days.** A 3-day all-day event is
  filed under its `startDate` only, not under each day it spans.
