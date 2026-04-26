# Linear Storage Protocol

Pigeon stores Linear issue tracker data as plain text JSONL files,
greppable with standard tools. This protocol covers two data types:
issues and comments. All files are UTF-8 encoded.

This document describes the on-disk wire format, directory layout, and
sync behaviour. It is a contract for what lands on disk, not a code
reference — anything a reader or external tool needs to understand to
work with pigeon's Linear storage should be here.

## Storage Philosophy

Linear data follows the same raw-storage pattern as Google Calendar
events and Drive comments: each JSONL line is the **raw CLI JSON
output** plus a single injected `type` discriminator. Pigeon does not
pick a subset of "interesting" fields — the full JSON response from the
`linear` CLI lands on disk verbatim. This makes storage lossless against
any field we don't currently use and future-proof against schema changes
in the Linear API.

The `linear` CLI is the sole interface to the Linear API. Pigeon shells
out to it for all data fetching — there is no direct GraphQL or HTTP
client code. Authentication, pagination, and API versioning are the
CLI's responsibility.

## Deduplication Rule

All JSONL line types use the same rule as the rest of pigeon:
**deduplicate by ID, keep last occurrence.**

- **Issues:** Each poll appends a fresh snapshot of every updated issue.
  Dedup by `id` keeps only the most recent snapshot. Earlier snapshots
  are redundant — the latest one has the current state, assignee,
  labels, and priority.
- **Comments:** Comments are immutable in Linear (edits update
  `updatedAt` but the `id` stays the same). Dedup by `id` keeps the
  latest version of each comment.

## Polling and Sync

Linear has no real-time event stream. All sync is poll-based, using the
`linear` CLI as a subprocess.

| Data type | CLI command | Cursor |
|-----------|-------------|--------|
| Issues | `linear issue query -j --all-teams --all-states --updated-after=<cursor>` | `updatedAt` timestamp |
| Comments | `linear issue view <identifier> -j --no-download` | Per-issue, fetched when the issue is updated |

### First-Run Backfill

Linear issues are mutable entities — an issue created months ago may
still be actively worked on, while a completed issue from last month is
probably irrelevant. The backfill fetches two sets:

1. **All active issues** (any state except completed/canceled):

   ```
   linear issue query -j --all-teams -s triage -s backlog -s unstarted -s started --limit=0
   ```

2. **Recently closed issues** (completed or canceled in the last 90 days):

   ```
   linear issue query -j --all-teams -s completed -s canceled --updated-after=<now-90d> --limit=0
   ```

`--limit=0` disables the default 50-issue cap. The CLI handles
pagination internally and returns all results.

This gives complete visibility into open work plus recent history,
without fetching issues that were closed long ago. The 90-day window
matches the backfill depth used by GWS sources (Gmail, Drive, Calendar).

For each issue returned, the poller also fetches the full issue view
(which includes comments) and writes everything to disk. The cursor is
then seeded with the maximum `updatedAt` across both batches.

Backfill writes data to disk before the cursor is saved. If interrupted,
re-running starts over — idempotency comes from the keep-last-by-ID
dedup rule.

### Incremental Sync

Each poll cycle:

1. Load cursor (last `updatedAt` timestamp) from `.sync-cursors.yaml`.
2. Run `linear issue query -j --all-teams --all-states --updated-after=<cursor> --limit=0`.
3. For each returned issue:
   a. Compute `<date>` from the issue's `updatedAt` (UTC).
   b. Append the issue snapshot to `issues/{IDENTIFIER}/<date>.jsonl`.
   c. Run `linear issue view <identifier> -j --no-download` to get the
      full issue with comments.
   d. Append all current comments to the same date file.
4. Update the cursor to the maximum `updatedAt` from the batch.
5. Save the cursor.

The `--updated-after` flag is the incremental cursor. It returns issues
whose `updatedAt` is strictly after the given timestamp, so the poller
never misses updates and never re-fetches unchanged issues.

### Comment Fetching

Comments are fetched as part of `linear issue view`, which returns the
full issue including all comments. The poller only calls `issue view`
for issues that appear in the incremental query results (i.e. issues
that changed since the last poll).

Comments are nested under `comments.nodes[]` in the view response. Each
comment has an `id`, and the dedup rule (keep last by ID) handles
re-appends of already-seen comments.

Threaded comments have a `parent.id` field pointing to their parent
comment. Top-level comments have `parent: null`. Thread structure is
preserved on disk but is a display-time concern, not a storage concern.

### Poll Interval and Rate Limits

The poller runs every 30 seconds — same order of magnitude as GWS (20s).

Linear's API rate limits (per API key): **5,000 requests/hour** and
**250,000 complexity points/hour**, using a leaky bucket that refills at
~83 requests/min. Each poll cycle makes 1 + N API calls (1 query + N
issue views for changed issues). Budget analysis:

| Scenario | Calls/hr | % of request budget |
|----------|---------|---------------------|
| Quiet (0 changes) | 120 | 2.4% |
| Normal (5 issues change) | 130 | 2.6% |
| Active (20 issues change) | 160 | 3.2% |
| Backfill (200 issues) | 201 | 4.0% |

Steady-state polling uses well under 5% of the budget. The incremental
cursor means quiet cycles are a single lightweight query. Even backfill
of hundreds of issues stays within limits. The complexity point budget
(250k/hr) is even less of a concern — a 10-issue query costs ~14 points.

Linear recommends webhooks over polling, but since pigeon wraps the CLI
(not a server), webhooks aren't an option. The polling footprint is
negligible.

### Cursor Expiry

The `--updated-after` cursor is a plain ISO 8601 timestamp, not an
opaque server token. It does not expire. However, if the cursor is very
old (e.g. weeks), the first incremental poll may return a large batch.
This is handled the same as backfill — process all results, dedup on
disk.

## Directory Layout

```
~/.local/share/pigeon/
└── linear-issues/                          # platform
    └── {workspace-slug}/                   # e.g. my-team
        ├── .sync-cursors.yaml              # cursor state
        └── issues/
            ├── ENG-101/                    # one directory per issue
            │   ├── 2026-04-06.jsonl        # ENG-101 activity on Apr 6
            │   └── 2026-04-07.jsonl
            ├── ENG-142/
            │   └── 2026-04-07.jsonl
            └── ENG-205/
                └── 2026-04-05.jsonl
```

### Why Per-Issue Date-Sharded Files

Each issue is its own append-only event stream — issue snapshots and
comments accumulate over time. Sharding that stream by UTC date matches
the messaging convention so the same date-based discovery selectors
(`*YYYY-MM-DD.jsonl` globs) find recently-updated issues by filename
without scanning content.

The date used for sharding is the date portion of the issue's
`updatedAt`. A poll cycle writes the issue snapshot plus all current
comments into a single file representing "issue state observed at this
updatedAt". Comments fetched alongside an updated issue land in the
same date file as the snapshot, even if they were originally created on
earlier days — the grouping is "what the poller observed" rather than
"when each line was authored".

This makes `pigeon list --since=Nd` and `pigeon glob --since=Nd`
discover Linear issues by date filename like every other source, and
`pigeon read linear ENG-101` a directory walk over the issue's date
files.

### Multiple Workspaces

Each Linear workspace is scoped under `linear-issues/{workspace-slug}/` with
independent cursors. The poller iterates over all configured workspaces
on each cycle.

### Workspace Slugs

The workspace slug comes directly from Linear's workspace slug (e.g.
`my-team`). It is used as-is without further slugification since Linear
slugs are already URL-safe.

### Cursor File

Path: `linear-issues/{workspace-slug}/.sync-cursors.yaml`

```yaml
issues:
  updated_after: "2026-04-11T14:30:00.000Z"
```

A single cursor for all issue data. The timestamp is the maximum
`updatedAt` seen across all issues in the last successful poll.

## Line Types

### Issue Line

Each line is the **raw `linear issue query` JSON for one issue** plus a
`"type":"linear-issue"` discriminator. Only the `type` key is injected by
pigeon; every other field is verbatim from the CLI output.

```json
{"type":"linear-issue","id":"c610f566-fc1d-40db-b129-8070743f9559","identifier":"ENG-142","title":"Fix login timeout on slow connections","url":"https://linear.app/my-team/issue/ENG-142/fix-login-timeout","priority":2,"priorityLabel":"High","estimate":null,"createdAt":"2026-04-02T15:14:52.509Z","updatedAt":"2026-04-05T09:44:15.076Z","state":{"id":"b9daaf2f-adae-4990-9c77-0a9170de7ef0","name":"In Progress","color":"#f2c94c","type":"started"},"assignee":{"id":"9a3fcead-a961-4dfd-9360-6b8b9b069b51","name":"Alice Smith","displayName":"alice","initials":"AS"},"team":{"id":"faa02806-b9fa-424d-9a87-b18d52a64ef8","key":"ENG","name":"Engineering"},"project":{"id":"29916b1d-0dbc-457d-a9b3-dafb16615a72","name":"Q2 Reliability"},"projectMilestone":null,"cycle":null,"labels":{"nodes":[]}}
```

Fields callers commonly rely on (the rest are preserved but may or may
not be interesting):

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"linear-issue"` | Storage discriminator (injected, not from CLI) |
| `id` | string | Linear issue UUID (dedup key) |
| `identifier` | string | Human-readable identifier (e.g. `ENG-142`) — used as filename |
| `title` | string | Issue title |
| `url` | string | Linear web URL |
| `createdAt` | RFC 3339 | When the issue was created |
| `updatedAt` | RFC 3339 | When the issue was last modified (cursor field) |
| `state.name` | string | Current state (`Todo`, `In Progress`, `Done`, etc.) |
| `state.type` | string | State category (`triage`, `backlog`, `unstarted`, `started`, `completed`, `canceled`) |
| `assignee.name` | string | Assignee display name (null if unassigned) |
| `team.key` | string | Team identifier (e.g. `ENG`) |
| `project.name` | string | Project name (null if not in a project) |
| `labels.nodes[]` | array | Labels with `name` and `color` |
| `priority` | int | Priority (0 = no priority, 1 = urgent, 2 = high, 3 = medium, 4 = low) |

### Comment Line

Each line is the **raw comment JSON from `linear issue view`** plus a
`"type":"linear-comment"` discriminator. Only the `type` key is injected by
pigeon; every other field is verbatim from the CLI output.

```json
{"type":"linear-comment","id":"0bb50b07-3f72-4412-ad63-e6aca4dd5dea","body":"Looks good — can we add a retry on timeout?","createdAt":"2026-04-08T14:04:31.883Z","url":"https://linear.app/my-team/issue/ENG-142/fix-login-timeout#comment-0bb50b07","resolvedAt":null,"user":{"name":"Bob Jones","displayName":"bob"},"externalUser":null,"parent":null}
```

Fields callers commonly rely on:

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"linear-comment"` | Storage discriminator (injected, not from CLI) |
| `id` | string | Linear comment UUID (dedup key) |
| `body` | string | Comment text (markdown) |
| `createdAt` | RFC 3339 | When the comment was posted |
| `url` | string | Direct link to the comment |
| `user.name` | string | Author display name |
| `user.displayName` | string | Author username |
| `parent.id` | string | Parent comment ID (null for top-level comments) |
| `resolvedAt` | RFC 3339 / null | When the comment thread was resolved |

### Line Ordering Within a File

Lines in an issue file are ordered chronologically by write time. A
typical file looks like:

```jsonl
{"type":"linear-issue",...,"updatedAt":"2026-04-02T15:14:52Z",...}
{"type":"linear-comment",...,"createdAt":"2026-04-07T09:28:55Z",...}
{"type":"linear-comment",...,"createdAt":"2026-04-07T10:36:38Z",...}
{"type":"linear-comment",...,"createdAt":"2026-04-07T11:32:18Z",...}
{"type":"linear-issue",...,"updatedAt":"2026-04-08T14:37:10Z",...}
{"type":"linear-comment",...,"createdAt":"2026-04-08T14:04:31Z",...}
{"type":"linear-comment",...,"createdAt":"2026-04-08T14:37:10Z",...}
```

Issue snapshot lines appear whenever the poller detects a change (state
transition, assignee change, label update, new comment, etc.). Comment
lines appear interleaved at their write time. After dedup (keep last by
ID), the file reduces to the latest issue snapshot and the full set of
unique comments.

## Go Type Definitions

Linear types follow the same dual-representation pattern as
`CalendarEvent` and `DriveComment`:

```go
// LinearIssue holds the raw CLI JSON (Serialized) and a minimal
// parsed struct (Runtime) for dedup and cursor extraction.
type LinearIssue struct {
    Runtime    LinearIssueRuntime
    Serialized map[string]any
}

type LinearIssueRuntime struct {
    ID         string `json:"id"`
    Identifier string `json:"identifier"`
    UpdatedAt  string `json:"updatedAt"`
}

// LinearComment holds the raw CLI JSON (Serialized) and a minimal
// parsed struct (Runtime) for dedup.
type LinearComment struct {
    Runtime    LinearCommentRuntime
    Serialized map[string]any
}

type LinearCommentRuntime struct {
    ID        string `json:"id"`
    CreatedAt string `json:"createdAt"`
}
```

The Runtime structs are intentionally minimal — only the fields needed
for dedup (`id`), cursor tracking (`updatedAt`), and file routing
(`identifier`). Everything else lives in Serialized and round-trips
through disk unchanged.

This matches the existing pattern where `CalendarEvent.Runtime` is
`calendar.Event` (a large SDK type) and `DriveComment.Runtime` is
`drive.Comment`. The difference is that Linear types don't use an SDK
struct for Runtime — the `linear` CLI returns arbitrary JSON, not a
typed Go struct, so the Runtime is a small hand-written struct with
only the fields pigeon needs.

## Read Protocol Integration

Linear appears as a new source in the read protocol:

| Source | Data type | Platform |
|--------|-----------|----------|
| `linear` | Issues and comments | Linear |

### Selector

The selector is an issue identifier:

```
pigeon read linear ENG-101            # specific issue + comments
pigeon read linear                    # recent issue activity (all issues)
```

When no selector is given, the reader shows recently updated issues
(equivalent to `pigeon list --source=linear --since=7d`).

Selector matching is exact on the identifier prefix (e.g. `ENG-101`)
and fuzzy on the title. `pigeon read linear "deploy"` matches issues
whose title contains "deploy".

### Filters

| Filter | Flag | Description |
|--------|------|-------------|
| Time window | `--since=DURATION` | Issues updated within duration |
| Specific date | `--date=YYYY-MM-DD` | Issues updated on a specific day |
| State | `--state=STATE` | Filter by state category (`started`, `completed`, etc.) |

Default when no filter: issues updated in the last 7 days.

### Read Algorithm

**Single issue** (`pigeon read linear ENG-101`):

1. Read every `issues/ENG-101/*.jsonl` date file.
2. Deduplicate by `id` (keep last) — reduces to latest issue snapshot +
   all unique comments.
3. Display: issue metadata (title, state, assignee, project, labels),
   then description (from the `issue view` data if available), then
   comments in chronological order. Threaded comments (those with
   `parent.id`) are indented under their parent.

**All issues** (`pigeon read linear --since=7d`):

1. Use the date-glob discovery (same selector messaging uses) to find
   `issues/*/YYYY-MM-DD.jsonl` files within the window.
2. Group by per-issue directory; for each issue, take the latest issue
   line across all date files in the window.
3. Sort by `updatedAt` descending (most recently active first).
4. Display as a list: identifier, title, state, assignee, last update.

### Context Integration

Linear workspaces appear in contexts alongside other platforms:

```yaml
contexts:
  work:
    gws: work@company.com
    slack: acme-corp
    linear: my-team
```

## Greppability

Standard text tools work directly on the JSONL files:

```bash
# Find all issues mentioning "deploy"
rg "deploy" ~/.local/share/pigeon/linear-issues/

# Find issues assigned to alice
rg '"displayName":"alice"' ~/.local/share/pigeon/linear-issues/my-team/issues/

# Find all In Progress issues
rg '"name":"In Progress"' ~/.local/share/pigeon/linear-issues/my-team/issues/

# Find comments by bob
rg '"name":"Bob' ~/.local/share/pigeon/linear-issues/my-team/issues/

# Count lines per issue (proxy for activity)
for d in ~/.local/share/pigeon/linear-issues/my-team/issues/*/; do
    wc -l "$d"/*.jsonl
done
```

Because issues and comments are stored as raw CLI JSON, any field the
Linear API returns is grep-able. Adding a new query doesn't require a
code change — `jq` or `grep` on the stored lines is enough.

### Pigeon Search Integration

The `pigeon grep` command includes Linear files in search globs:

```
pigeon grep "deploy" --source=linear --since=7d
```

## Maintenance

A per-issue date file accumulates duplicate snapshots when an issue
changes more than once on the same day (one snapshot per poll cycle that
saw the change). Maintenance compacts within a date file, and compaction
extends across the per-issue directory at read time:

1. Deduplicate issue lines by `id` (keep last) — removes stale
   snapshots, keeping only the latest.
2. Deduplicate comment lines by `id` (keep last) — removes duplicates
   from re-fetches.
3. Rewrite each date file with the deduplicated lines in chronological
   order; cross-file dedup happens at read time.

Maintenance is lightweight because individual date files are small
(tens of lines per active day). It can run opportunistically without
blocking reads or writes.

## Configuration

Linear workspaces are configured in `config.yaml`:

```yaml
linear:
  - workspace: my-team           # Linear workspace slug
    account: my-team             # display name for pigeon
```

The `workspace` field is the Linear workspace slug (used for
`--workspace` flag on CLI calls). The `account` field is the display
name shown by `pigeon list` and used for directory naming (though in
practice it will usually be the same as the workspace slug).

## Known Limitations

- **No real-time events.** Linear has webhooks but pigeon uses the CLI
  wrapper, which is poll-based. Updates are delayed by at most one poll
  interval (30s).
- **Comment edits may be missed between polls.** If a comment is edited
  and then edited again between two polls, only the final state is
  captured. This is acceptable — the latest state is what matters.
- **Issue descriptions are only available from `issue view`.** The
  `issue query` response does not include the description field. The
  description is fetched separately via `issue view` for each updated
  issue. If the description is very long, the view call is heavier.
- **No attachment download.** Issue attachments and comment images are
  stored as URLs. The actual files are not downloaded in V1.
- **`linear issue query` has no `--no-pager` on some subcommands.**
  The poller must handle this (e.g. pipe through `cat` or set
  `PAGER=cat`).
- **Documents are out of scope.** Linear documents exist but the
  workspace has none and the feature is rarely used. Support can be
  added later following the same raw-storage pattern.
- **Projects are out of scope.** Project data (status, updates) would
  be useful but lacks incremental filtering in the CLI. Can be added
  later.
