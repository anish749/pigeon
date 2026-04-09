# Gmail Backfill — Architecture Decision Record

## Problem

The V1 Gmail sync seeded with `users.getProfile` (historyId to "now")
and only captured future messages. A fresh setup started with an empty
inbox view.

## What was built

Gmail backfill on first run via `messages.list` with a date query,
followed by individual `messages.get` for each message. Same cursor-
first pattern as Drive backfill.

## Design

### Backfill via `messages.list`

Gmail has no equivalent of Drive's `files.list(modifiedTime > ...)`.
Instead, `messages.list` accepts a Gmail search query (`q` parameter)
with date operators:

```
q=after:2026/01/08
```

This returns message IDs only — each message requires an individual
`messages.get(format=raw)` call for content. There is no batch get
API for messages.

### Seed flow

1. `users.getProfile` → acquire `historyId` BEFORE backfill starts
2. `messages.list(q="after:YYYY/MM/DD")` → paginate → collect all
   message IDs
3. For each message: `GetMessage(id)` → parse MIME → store JSONL
4. Save `historyId`

The historyId is acquired first because backfill can take 20+ minutes
for a large inbox. Messages arriving during backfill are captured by
the first incremental poll.

### Volume and performance

Validated against a real inbox:

| Metric | Value |
|---|---|
| Messages in 90-day window | 1,191 |
| `messages.list` pages | 3 (500 per page) |
| Per-message `messages.get` | ~1 second (gws CLI overhead) |
| Estimated backfill time | ~20 minutes |
| API units per message | 5 (messages.get) |
| API units per minute | ~300 (60 messages × 5 units) |
| Per-user budget | 15,000 units/min |
| Budget utilization | 2% |

The bottleneck is gws CLI process overhead (~1 second per call), not
API rate limits. Direct API calls would be ~100x faster, but the CLI
architecture is a V1 constraint shared across all three services.

### Progress logging

Backfill logs progress every 100 messages so the user can see it's
working during a 20-minute backfill:

```
INFO gmail backfill progress fetched=100 total=1191
INFO gmail backfill progress fetched=200 total=1191
...
```

### Shared message fetch logic

`fetchAndStoreMessages` was extracted from the incremental poll path
and is now shared between `seedGmail` (backfill) and `PollGmail`
(incremental). Both use the same fetch → parse → store pipeline.
Messages deleted between enumeration and fetch are skipped with
`slog.Debug` (same race handling as incremental).

### No extra cursor state needed

Like Drive, Gmail backfill needs no additional cursor state. The
`historyId` cursor never expires conceptually (though Gmail returns
404 for very old historyIds, handled by `IsCursorExpired`). The
cursor structure is unchanged:

```yaml
gmail:
  history_id: "12975259"
```

### Duplicate handling

Messages fetched during backfill may also appear in the first
incremental poll (if they arrived between `getProfile` and the first
`ListHistory` call). This is handled by keep-last dedup on read —
duplicate messages are identical, so the order doesn't matter.

## Alternatives considered

### Skip backfill, rely on history API

Gmail's `history.list` could theoretically go back in time if we used
an old `historyId`. But `getProfile` only returns the current
`historyId`, and there's no API to get a historical one. The
`historyId` also expires (404) after an unspecified period, making
this unreliable.

### Parallel message fetching

Fetching messages concurrently would speed up backfill significantly
(the API budget allows 3,000 messages/min). Not implemented because
the gws CLI spawns a new process per call and concurrent process
spawning adds complexity. Can be added later as an optimization.
