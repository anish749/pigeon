# Calendar Backfill & Recurring Events — Architecture Decision Record

## Problem

The V1 calendar sync (PR #94) seeded cursors to "now" and only captured
future changes. A fresh setup started with an empty calendar view.
Recurring events were stored as parent events with RRULEs — a weekly
standup appeared as a single event, with future instances invisible to
grep.

## What was built

PR #135 adds three-phase calendar sync: historical backfill on first
run, incremental sync with recurring event expansion, and automatic
window extension as time passes. All recurring events are expanded to
individual instances on disk for greppability.

## Design

### Why not `singleEvents=true`

The Calendar API's `singleEvents=true` parameter expands recurring
events server-side. This was tested and rejected for two reasons:

**Infinite expansion without `timeMax`.** With `singleEvents=true` and
no upper time bound, the API expands recurring events decades into the
future. A calendar with ~40 recurring events produced 12,750+ events
paginating into 2049 before the test was stopped at 51 pages. This is
not bounded by Google — the API will keep paginating.

**Sync token scoping with `timeMax`.** Adding `timeMax` bounds the
expansion but creates a permanent blind spot. The sync token returned
by a time-bounded query is scoped to that window — events outside the
original `timeMin`/`timeMax` are not tracked by incremental sync. This
was validated: the API returns HTTP 400 if you pass `timeMin`/`timeMax`
alongside a `syncToken`.

The critical failure mode: a recurring weekly meeting has instances
beyond `timeMax` that were never materialized during the seed. Since
they haven't "changed" (they're just future occurrences of an existing
rule), the sync token never reports them. The event is invisible until
a re-seed.

A separate test confirmed that *newly created* events outside the
original time window ARE picked up by incremental sync — but that's
creation, not recurring expansion. The distinction matters.

### Chosen approach: `singleEvents=false` + `events.instances`

The sync uses `singleEvents=false` (the default) for the main
`events.list` calls. This returns parent recurring events with their
recurrence rules, one-off events, and exception instances. The sync
token from this query is calendar-wide — it tracks all changes
regardless of time window, with no scoping problem.

Recurring events are then expanded client-side by calling
`events.instances` per parent event, bounded to a ±90-day window.
This is an n+1 call pattern (1 `events.list` + N `events.instances`),
but N is typically small (43 recurring events in the test calendar)
and each `events.instances` call returns a bounded result set (~26
instances for a weekly event over 180 days).

### Three-phase sync cycle

Every 20-second poll tick runs these phases in order:

**Phase 1 — Seed (no cursor exists):**
1. `events.list(singleEvents=false, timeMin=now-90d)` → paginate →
   collect all events + `syncToken`
2. Classify each event: parent recurring (has `recurrence` field),
   exception instance (has `recurringEventId`), or one-off
3. Write one-off events and exception instances to disk
4. For each parent: `events.instances(eventId, timeMin=now-90d,
   timeMax=now+90d)` → write expanded instances to disk
5. Save cursor: `sync_token`, `expanded_until=now+90d`,
   `recurring_events=[list of parent IDs]`

**Phase 2 — Incremental sync (cursor exists):**
1. `events.list(syncToken=...)` → get changed events
2. Dispatch by type:
   - One-off event → write to disk
   - Recurring parent (has `recurrence`) → re-expand via
     `events.instances(eventId, timeMin=now-90d, timeMax=expanded_until)`
     → overwrite instances on disk. Add to `recurring_events` if new.
   - Instance change (has `recurringEventId`) → write directly to disk
     (dedup keeps latest)
3. Save new `sync_token`

**Phase 3 — Window expansion (`expanded_until` approaching):**
1. Check: is `expanded_until` < now + 30 days?
2. If yes, for each ID in `recurring_events`:
   `events.instances(eventId, timeMin=expanded_until, timeMax=now+90d)`
   → write new instances to disk
3. Update `expanded_until = now+90d`

Phase 3 runs within the normal 20-second poll cycle — no separate
scheduler. It triggers roughly every 60 days (when the 90-day window
is within 30 days of now).

### Cursor structure

The calendar cursor expanded from a simple sync token string to a
struct tracking three pieces of state:

```yaml
calendar:
  primary:
    sync_token: "CJ_2-OT13pMD..."
    expanded_until: "2026-07-07T22:05:12Z"
    recurring_events:
      - "ql2e63sqv9o8jb46grq4msqijc"
      - "e27b9a1sjhrn69l7bfatv4ultc"
```

- **sync_token**: Google's incremental sync cursor, calendar-wide
- **expanded_until**: the `timeMax` used for the last recurring event
  expansion, used as `timeMin` for the next window extension
- **recurring_events**: IDs of all known parent recurring events, so
  window expansion can re-expand without another `events.list`

### Incremental sync behavior for recurring events

Validated against the live API: when a single instance of a recurring
event is modified, the incremental sync returns both the parent
recurring event AND the modified instance (plus other exception
instances). The parent has `recurrence=[RRULE:...]` and no
`recurringEventId`; instances have `recurringEventId` and no
`recurrence`.

This means the poller sees the parent in the sync response → triggers
re-expansion of that event's instances → overwrites all instances on
disk with current data. The individually modified instance is also
written directly from the sync response. Dedup (keep-last by ID)
ensures the latest version wins regardless of write order.

### Event classification

The `classify` function in `calendar/client.go` separates raw API
events into two categories based on a single field:

- **Has `recurrence` field** → recurring parent → collect ID for
  expansion, do not write to disk (instances are what's greppable)
- **Everything else** → one-off event or instance → write to disk

Parent recurring events are not written to disk because they are
templates, not concrete events. The expanded instances carry all the
useful data (specific date, time, any per-instance modifications).

### API cost

| Phase | When | API calls |
|-------|------|-----------|
| Seed | First run | 1 paginated `events.list` + N `events.instances` |
| Incremental | Every 20s | 1 `events.list` + M `events.instances` (M = changed parents, usually 0) |
| Window expansion | Every ~60 days | N `events.instances` |

For a calendar with 43 recurring events, the seed makes 44 API calls.
Incremental polls are 1 call (quiet) or 2-3 calls (when a recurring
event changes). Window expansion is 43 calls every ~60 days.

All calls are within the per-user Calendar quota of ~600 queries/min.

### Sync token expiry

If `IsCursorExpired(err)` (HTTP 410 or 400/invalid), the entire
calendar cursor is cleared — `sync_token`, `expanded_until`, and
`recurring_events` are all reset. The next poll cycle triggers a
full re-seed (Phase 1).

## Alternatives considered

### `singleEvents=true` with `timeMax` + periodic re-seed

Seed with `singleEvents=true`, `timeMin=now-90d`, `timeMax=now+90d`.
Discard the sync token every 90 days and re-seed with a shifted window.
Rejected because:
- The sync token is scoped to the time window — events outside the
  original window are invisible between re-seeds
- Re-seeding discards the sync token, requiring a full re-fetch
- Recurring event instances beyond `timeMax` that haven't been
  individually modified are a permanent blind spot until re-seed

### `singleEvents=true` without `timeMax`

Use `singleEvents=true` to get server-side expansion, relying on Google
to bound the output. Rejected because Google does not bound it — tested
with a real calendar, the API expanded recurring events into 2049 and
beyond, producing 12,750+ events before the test was stopped.

### Client-side RRULE expansion without `events.instances`

Store parent events with RRULEs, expand at read time using an RRULE
library. Rejected because:
- Defeats the greppability goal (grep can't find "standup on Tuesday"
  if only the RRULE template exists on disk)
- RRULE expansion is non-trivial (timezones, exceptions, modifications)
- The `events.instances` API handles all edge cases (modified instances,
  cancelled instances, timezone changes) and is already validated

### Batch `events.instances` for multiple event IDs

Tested: `events.instances` does not support multiple event IDs in a
single call. Passing comma-separated IDs returned 0 results. Each
recurring event requires its own API call.

## Test coverage

`TestCalendarBackfillLive` (gated behind `GWS_LIVE_TEST=1`) runs the
full lifecycle against the real Google Calendar API:
1. Creates a one-off event and a recurring event (DAILY;COUNT=5)
2. Seeds calendar (Phase 1) — verifies cursor state, recurring event
   tracking, and expanded instances on disk
3. Patches one instance, runs incremental sync (Phase 2) — verifies
   the modification appears on disk
4. Runs a quiet poll (Phase 3 without window expansion) — verifies no
   errors on a no-change cycle
5. Cleans up all created test events
