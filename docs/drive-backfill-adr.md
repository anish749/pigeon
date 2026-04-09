# Drive Backfill & Comments Overwrite — Architecture Decision Record

## Problem

The V1 Drive sync (PR #94) seeded with `changes.getStartPageToken` and
only captured future changes. Existing Docs, Sheets, and their comments
were invisible on first run. Additionally, `storeComments` appended all
comments on every file change, causing unbounded JSONL growth.

## What was built

PR #136 adds Drive backfill using `files.list` to enumerate existing
Docs and Sheets on first run, and fixes comments to use full-snapshot
overwrite instead of append.

## Design

### Backfill via `files.list`

The Drive Changes API (`changes.list`) cannot go back in time —
`getStartPageToken` returns a cursor for future changes only. Backfill
requires a different API: `files.list` with a `modifiedTime` filter.

The query:
```
modifiedTime > '{now-90d}'
  and (mimeType = 'application/vnd.google-apps.document'
    or mimeType = 'application/vnd.google-apps.spreadsheet')
  and trashed = false
```

Results are ordered by `modifiedTime desc` (most recent first). Each
returned file feeds into the existing `handleDoc`/`handleSheet` pipeline
unchanged — no new per-file logic needed.

### Seed flow

1. `changes.getStartPageToken` → acquire cursor BEFORE backfill starts
2. `files.list` with `modifiedTime > now-90d` → paginate → collect all
   Docs and Sheets
3. For each file: `handleDoc` or `handleSheet` (fetch content, convert,
   store comments, write to disk)
4. Save `pageToken`

The cursor is acquired first because backfill can take minutes (one API
call per file × content + comments). Any files modified during backfill
are captured by the first incremental poll since the cursor predates the
backfill window.

The page token is seeded AFTER backfill so that any files modified
between `files.list` and `getStartPageToken` are caught by the first
incremental poll (re-exported, overwrite handles dedup).

### No extra cursor state needed

Unlike Calendar (which needs `expanded_until` and `recurring_events`),
Drive backfill requires no additional cursor state. The `pageToken`
never expires, and files don't "expand" over time like recurring events.
The cursor structure is unchanged:

```yaml
drive:
  page_token: "1339021"
```

### Comments overwrite

The Drive Comments API has no incremental sync — no cursor, no
`modifiedTime` filter, no sync token. Every call to `ListComments`
returns the full set of comments for a file.

Previously, `storeComments` appended every comment to `comments.jsonl`
on every file change. A doc with 200 comments edited 50 times produced
10,000 lines where 200 were unique.

Now, `storeComments` uses `WriteLines` which replaces the entire file
contents with the current snapshot. This handles all comment lifecycle
states correctly:

| Scenario | Append (old) | Overwrite (new) |
|---|---|---|
| New comment | Appended ✓ | In snapshot ✓ |
| Resolved comment | Old `resolved: false` stays | Only `resolved: true` |
| Deleted comment | Old line stays forever | Gone — not in snapshot |
| Modified comment | Both versions on disk | Only current version |

`WriteLines` was added to `gwsstore` alongside `AppendLine` and
`ReadLines`. It marshals all lines to bytes and writes atomically
with `os.WriteFile`.

### Difference from Calendar backfill

| Aspect | Calendar | Drive |
|---|---|---|
| Backfill API | Same API with different params | Different API (`files.list`) |
| Cursor expiry | syncToken expires (410/400) | pageToken never expires |
| Window expansion | Needed (recurring events) | Not needed |
| Extra cursor state | `expanded_until`, `recurring_events` | None |
| Comment sync | N/A | Full snapshot overwrite |

### Race condition: file modified during backfill

The changes cursor is acquired before `files.list` runs. Any file
modified during the backfill window is captured by the first
incremental `changes.list` poll, which triggers a re-export. Content
files are overwritten (`.md`, `.csv`), and comments are a full
snapshot — both are idempotent. Files may be exported twice (once
during backfill, once during the first incremental poll), but this is
correct and harmless.

### API cost

| Phase | API calls |
|---|---|
| Backfill seed | 1 `files.list` (paginated) + N × (~3 calls per file) |
| Incremental | 1 `changes.list` + per-changed-file exports |

For a typical account: 17 files × 3 calls = ~51 API calls during seed.
Well within the 12,000 queries/min per-user Drive quota.

## Test coverage

`TestDriveBackfillLive` (gated behind `GWS_LIVE_TEST=1`) creates a
test doc BEFORE seeding, runs backfill, and verifies the doc lands on
disk with `.md` content and `meta.json`. Then runs a quiet incremental
poll to verify no errors.

`TestWriteLines` and `TestWriteLinesEmpty` verify the overwrite
semantics of the new `WriteLines` function — replacing contents and
handling empty input.
