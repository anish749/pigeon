# Jira Storage Protocol

Pigeon stores Jira issue tracker data as plain text JSONL files,
greppable with standard tools. This protocol covers two data types:
issues and comments. All files are UTF-8 encoded.

This document describes the on-disk wire format, directory layout, and
sync behaviour. It is a contract for what lands on disk, not a code
reference — anything a reader or external tool needs to understand to
work with pigeon's Jira storage should be here.

## Storage Philosophy

Jira data follows the same raw-storage pattern as Linear, Google
Calendar events, and Drive comments: each JSONL line is the **raw CLI
JSON output** plus a single injected `type` discriminator. Pigeon does
not pick a subset of "interesting" fields — the full JSON response from
the `jira` CLI lands on disk verbatim. This makes storage lossless
against any field we don't currently use and future-proof against schema
changes in the Jira REST API.

The `jira` CLI
([ankitpokhrel/jira-cli](https://github.com/ankitpokhrel/jira-cli)) is
the sole interface to the Jira REST API. Pigeon shells out to it for
all data fetching — there is no direct HTTP client code.
Authentication, pagination of individual calls, cloud-vs-server API
version selection (v2/v3), and the Atlassian Document Format / markdown
round-trip are all the CLI's responsibility.

### Why `view --raw`, not `list --raw`

The CLI exposes two JSON modes but they are not equivalent:

- `jira issue view KEY --raw` prints the **raw HTTP response body** from
  `GET /rest/api/3/issue/{key}` (or v2 for server installations). Every
  field the Jira API returns — including fields pigeon never touches —
  is preserved. Comments live inside this response at
  `fields.comment.comments[]`.
- `jira issue list --raw` prints a **trimmed, CLI-parsed struct** — the
  `jira.Issue` Go type from
  [`pkg/jira/types.go`](https://github.com/ankitpokhrel/jira-cli/blob/main/pkg/jira/types.go).
  Many fields (attachments, custom fields, work log, votes, changelog,
  etc.) are dropped.

For lossless on-disk storage, pigeon **always writes the `view --raw`
JSON**, never the `list --raw` JSON. The list call is only used to
discover which issue keys changed; the view call is the source of
truth.

## Deduplication Rule

All JSONL line types use the same rule as the rest of pigeon:
**deduplicate by ID, keep last occurrence.**

- **Issues:** Each poll appends a fresh snapshot of every updated
  issue. Dedup by `id` keeps only the most recent snapshot. Earlier
  snapshots are redundant — the latest one has the current state,
  assignee, labels, priority, and description.
- **Comments:** Comments in Jira are mutable but keep a stable `id`
  across edits. Dedup by `id` keeps the latest version of each comment.

The numeric `id` (e.g. `"10042"`) is used as the dedup key, not the
human-readable `key` (e.g. `ENG-142`). The key can in principle change
if a project is moved or renamed in Jira; the `id` never does.

## Polling and Sync

Jira has webhooks but the `jira` CLI is HTTP-only, so all sync is
poll-based.

| Data type | CLI command | Cursor |
|-----------|-------------|--------|
| Issue keys | `jira issue list -q "updated > '<cursor>'" --plain --no-headers --columns key --paginate 0:100` | `updated` timestamp |
| Issue body + comments | `jira issue view <KEY> --raw` | Per-issue, fetched when the issue key appears in the list query |

Two CLI calls per changed issue: one list (discovery) + one view per
key (fetch). This mirrors how the Linear poller uses `issue query` +
`issue view`.

### Why Two Calls Per Issue

`jira issue list --raw` would give us one JSON blob for every changed
issue in a single HTTP round-trip, but — as noted under "Why
`view --raw`" above — its output is lossy. The alternative of writing
the trimmed form is worse than taking the extra HTTP call per issue:
losing fields silently on disk defeats the point of raw storage.

For the discovery step we therefore use `--plain --columns key` (a
simple tab-separated list of keys) rather than `--raw`. It's the
cheapest mode the CLI offers for "just give me the matching keys".

### First-Run Backfill

Jira issues, like Linear issues, are mutable entities. The backfill
fetches two sets:

1. **All active issues** (any status category except Done):

   ```
   jira issue list -q "statusCategory != Done" \
       --plain --no-headers --columns key --paginate 0:100
   ```

2. **Recently closed issues** (Done status category, updated in the
   last 90 days):

   ```
   jira issue list -q "statusCategory = Done AND updated > -90d" \
       --plain --no-headers --columns key --paginate 0:100
   ```

For each returned key, the poller runs `jira issue view <KEY> --raw`
and writes:

- One issue line (the top-level response with comments stripped).
- One comment line per entry in `fields.comment.comments[]`.

Pagination — the `--paginate` flag caps at 100 issues per call — is
handled by the poller, which loops incrementing the `from` offset until
an empty page returns. This differs from Linear, where `--limit=0`
opts into full server-side pagination; `jira` CLI has no equivalent.

The 90-day window for closed issues matches the backfill depth used by
other pigeon sources.

### Incremental Sync

Each poll cycle:

1. Load cursor (last `updated` timestamp) from `.sync-cursors.yaml`.
2. Run `jira issue list -q "updated > '<cursor>'" --plain --no-headers --columns key --paginate 0:100`,
   looping pages until exhausted.
3. For each returned key:
   a. Run `jira issue view <KEY> --raw`.
   b. Append the issue snapshot to `issues/{KEY}.jsonl` (with
      `fields.comment.comments` removed from the line).
   c. Append each comment as a separate `jira-comment` line to the
      same file.
4. Update the cursor to the maximum `fields.updated` across all issues
   fetched in this batch.
5. Save the cursor.

The JQL `updated > "<cursor>"` clause is the incremental cursor. It
returns issues whose `updated` is strictly after the given timestamp.
Only issues that actually changed since the last poll come back.

### Cursor Format

The cursor is stored as an ISO 8601 timestamp (RFC 3339, e.g.
`2026-04-11T14:30:00.000Z`) — the same format Jira returns in
`fields.updated`. But **JQL does not accept RFC 3339**: it only
understands the formats `yyyy-MM-dd HH:mm`, `yyyy/MM/dd HH:mm`,
`yyyy-MM-dd`, and `yyyy/MM/dd`. The poller converts the stored cursor
to `yyyy-MM-dd HH:mm` (UTC) before interpolating it into the JQL
string, and stores the original RFC 3339 form so no precision is lost
on round-trip.

JQL date filters are minute-precision. The poller rounds the cursor
*down* to the minute when formatting for JQL, which means a single
overlap minute may be re-fetched on the next poll. Dedup-on-ID handles
this without harm.

### Comment Fetching

Comments are fetched as part of `jira issue view --raw`, which returns
the full issue. The poller only calls `issue view` for keys that
appeared in the incremental list (i.e. issues that changed since the
last poll).

Comments are nested under `fields.comment.comments[]` in the view
response. Each comment has an `id`, and the dedup rule (keep last by
ID) handles re-appends of already-seen comments.

The `fields.comment` object also carries `total`, `startAt`, and
`maxResults`. On a very active issue, only the most recent N comments
(default 1,000 on Jira Cloud) come back in the view response. Pigeon
does not paginate comments — if a single issue has more than 1,000
comments, older ones will be missed. This matches the `jira-cli`
behaviour and is listed under Known Limitations.

Jira Cloud returns comment bodies as Atlassian Document Format (ADF)
JSON when using API v3. Server installations return them as wiki
markup strings. Pigeon stores whatever comes back verbatim — callers
are expected to handle both forms or route rendering through
`jira-cli`.

### Poll Interval and Rate Limits

The poller runs every 30 seconds — same as Linear.

Jira Cloud rate limits are dynamic, governed by a cost-based budget
per Atlassian site. There is no published flat number; the server
returns `X-RateLimit-Remaining` headers and, on exhaustion, HTTP 429
with `Retry-After`. Typical free-tier budgets are on the order of
thousands of requests per hour per user.

Each poll cycle makes `1 + N` API calls (1 list + N views for N
changed issues). Steady-state polling on a moderately active project
(5–20 changes per hour) is a few dozen calls per hour. This is well
inside any documented Atlassian rate limit.

If the CLI returns a non-zero exit code with stderr matching HTTP 429,
the poller logs an error, skips the cursor update, and retries on the
next tick. Linear handles rate-limit backoff inside its CLI; `jira`
does not, so pigeon treats 429s as transient errors and relies on the
30-second tick to space out retries.

### Cursor Expiry

The JQL `updated` cursor is a plain date string, not an opaque server
token. It does not expire. If the cursor is very old (weeks), the
first incremental poll may return a large batch spread across many
pages. This is handled the same as backfill — process all pages,
dedup on disk.

## Directory Layout

```
~/.local/share/pigeon/
└── jira-issues/                           # platform
    └── {site-slug}/                       # e.g. acme (from acme.atlassian.net)
        └── {project-key}/                 # e.g. ENG
            ├── .sync-cursors.yaml         # cursor state
            └── issues/
                ├── ENG-101.jsonl          # all activity for ENG-101
                ├── ENG-142.jsonl          # all activity for ENG-142
                └── ENG-205.jsonl
```

### Why Per-Issue Files

Same reasoning as Linear (see `linear-protocol.md`): Jira issues are
mutable entities with stable identifiers. Filing by date gives either
stale snapshots (file-by-created) or cross-file flutter
(file-by-updated). The natural unit is one file per issue, named by
the human-readable key.

`pigeon read jira ENG-101` becomes a direct file read, and
`pigeon grep "deploy" --source=jira` is a recursive grep.

### Multiple Sites and Projects

Unlike Linear (one workspace ≈ one team ≈ one issue namespace), Jira
lets a single Atlassian site host many projects with independent key
prefixes (`ENG-`, `OPS-`, `MKT-`, …). The `jira` CLI itself is
configured against a single site + single default project per config
file, switched via `JIRA_CONFIG_FILE` or the `-c/--config` flag.

Pigeon mirrors this by scoping cursors and storage to
`{site-slug}/{project-key}`. Each configured project gets an
independent cursor and directory. The poller iterates over all
configured (site, project) pairs on each cycle.

The site slug is derived from the Jira server URL — for
`https://acme.atlassian.net` the slug is `acme`. For on-premise
installations (e.g. `https://jira.internal.example.com`), the slug is
the lowercase first DNS label (`jira`), or, when that would collide
with another host, the first two labels joined with `-`
(`jira-internal`). Collision resolution is a config-time concern and
documented alongside the site's entry in `config.yaml`.

### Cursor File

Path: `jira-issues/{site-slug}/{project-key}/.sync-cursors.yaml`

```yaml
issues:
  updated_after: "2026-04-11T14:30:00.000Z"
```

A single cursor per project. The timestamp is the maximum `updated`
seen across all issues in the last successful poll, stored in RFC 3339
and rounded down to minute precision when formatted into JQL.

## Line Types

### Issue Line

Each line is the **raw `jira issue view --raw` JSON for one issue**
with `fields.comment.comments` removed, plus a `"type":"jira-issue"`
discriminator. The comment array is stripped out of the issue line
because comments are written as their own `jira-comment` lines —
keeping them nested would double-store comment bodies and complicate
dedup.

```json
{"type":"jira-issue","id":"10042","key":"ENG-142","self":"https://acme.atlassian.net/rest/api/3/issue/10042","fields":{"summary":"Fix login timeout on slow connections","issuetype":{"id":"10001","name":"Bug","subtask":false},"status":{"name":"In Progress","statusCategory":{"key":"indeterminate","name":"In Progress"}},"priority":{"name":"High"},"labels":["auth","perf"],"assignee":{"accountId":"5f...","displayName":"Alice Smith","emailAddress":"alice@acme.com"},"reporter":{"accountId":"6e...","displayName":"Bob Jones"},"resolution":null,"created":"2026-04-02T15:14:52.509+0000","updated":"2026-04-05T09:44:15.076+0000","description":{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Users on 3G report 30s timeouts."}]}]},"components":[{"name":"auth-service"}],"fixVersions":[],"issuelinks":[],"parent":{"key":"ENG-100","fields":{"summary":"Q2 Reliability"}},"comment":{"total":3,"maxResults":1000,"startAt":0}}}
```

Fields callers commonly rely on (the rest are preserved but may or may
not be interesting):

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"jira-issue"` | Storage discriminator (injected, not from CLI) |
| `id` | string | Jira issue numeric ID as a string (dedup key) |
| `key` | string | Human-readable key (e.g. `ENG-142`) — used as filename |
| `self` | string | Full REST API URL for this issue |
| `fields.summary` | string | Issue title |
| `fields.issuetype.name` | string | Issue type (`Bug`, `Story`, `Epic`, `Task`, `Sub-task`) |
| `fields.status.name` | string | Current status (`To Do`, `In Progress`, `Done`, …) |
| `fields.status.statusCategory.key` | string | Status bucket (`new`, `indeterminate`, `done`) |
| `fields.priority.name` | string | Priority name (`Highest`, `High`, `Medium`, `Low`, `Lowest`) |
| `fields.assignee.displayName` | string | Assignee display name (null if unassigned) |
| `fields.assignee.emailAddress` | string | Assignee email (may be absent on Cloud if the viewer lacks permission) |
| `fields.reporter.displayName` | string | Reporter display name |
| `fields.created` | timestamp | When the issue was created (see "Date Format" below) |
| `fields.updated` | timestamp | When the issue was last modified (cursor field) |
| `fields.labels` | array of string | Labels |
| `fields.components[]` | array | Components attached to the issue |
| `fields.fixVersions[]` | array | Fix-version (release) targets |
| `fields.resolution.name` | string | Resolution (`Fixed`, `Won't Do`, …) or null |
| `fields.parent.key` | string | Parent issue key (epic or story for sub-tasks) |
| `fields.description` | ADF object or string | Description body (ADF JSON on Cloud v3, wiki markup string on Server v2) |
| `fields.issuelinks[]` | array | Outward/inward links to other issues |

#### Date Format

Jira returns timestamps as RFC 3339 with millisecond precision and a
numeric offset, e.g. `2026-04-05T09:44:15.076+0000`. This is **not**
the canonical `Z` form that Linear uses. Cursor storage normalizes to
UTC and re-emits as `...Z` for consistency with the rest of pigeon,
but the raw `fields.updated` field on disk is left verbatim.

### Comment Line

Each line is one entry from `fields.comment.comments[]` in the
`issue view --raw` response, plus a `"type":"jira-comment"`
discriminator and the parent issue key for grep-ability. Everything
else is verbatim from the CLI output.

```json
{"type":"jira-comment","issueKey":"ENG-142","id":"10501","self":"https://acme.atlassian.net/rest/api/3/issue/10042/comment/10501","author":{"accountId":"6e...","displayName":"Bob Jones","emailAddress":"bob@acme.com"},"body":{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good — can we add a retry on timeout?"}]}]},"created":"2026-04-08T14:04:31.883+0000","updated":"2026-04-08T14:04:31.883+0000","jsdPublic":true}
```

Fields callers commonly rely on:

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"jira-comment"` | Storage discriminator (injected) |
| `issueKey` | string | Parent issue key (injected for convenience; not in the raw CLI output) |
| `id` | string | Comment ID (dedup key) |
| `self` | string | Full REST API URL for this comment |
| `body` | ADF object or string | Comment text (ADF JSON on Cloud, wiki markup on Server) |
| `author.displayName` | string | Author display name |
| `author.accountId` | string | Stable account ID (Cloud) |
| `created` | timestamp | When the comment was posted |
| `updated` | timestamp | When the comment was last edited |
| `jsdPublic` | bool | Visibility in Jira Service Desk — `false` means internal-only |

`issueKey` is the only field pigeon injects into comment lines beyond
`type`. It exists because the comment JSON as returned by the Jira API
does not reference its parent issue by key — only by the `self` URL.
Injecting `issueKey` makes cross-issue grep ("all comments mentioning
`deploy` in any issue") trivially filterable by issue.

### Line Ordering Within a File

Lines in an issue file are ordered chronologically by write time, the
same as Linear:

```jsonl
{"type":"jira-issue",...,"fields":{...,"updated":"2026-04-02T15:14:52+0000",...}}
{"type":"jira-comment",...,"created":"2026-04-07T09:28:55+0000",...}
{"type":"jira-comment",...,"created":"2026-04-07T10:36:38+0000",...}
{"type":"jira-issue",...,"fields":{...,"updated":"2026-04-08T14:37:10+0000",...}}
{"type":"jira-comment",...,"created":"2026-04-08T14:04:31+0000",...}
```

Issue snapshot lines appear whenever the poller detects a change
(status transition, assignee change, label update, new comment, etc.).
Comment lines appear interleaved at their write time. After dedup
(keep last by ID), the file reduces to the latest issue snapshot and
the full set of unique comments.

## Go Type Definitions

Jira types follow the same dual-representation pattern as Linear,
`CalendarEvent`, and `DriveComment`:

```go
// JiraIssue holds the raw CLI JSON (Serialized) and a minimal
// parsed struct (Runtime) for dedup, cursor extraction, and file routing.
type JiraIssue struct {
    Runtime    JiraIssueRuntime
    Serialized map[string]any
}

type JiraIssueRuntime struct {
    ID     string           `json:"id"`
    Key    string           `json:"key"`
    Fields JiraIssueFields  `json:"fields"`
}

type JiraIssueFields struct {
    Updated string `json:"updated"`
}

// JiraComment holds the raw CLI JSON (Serialized) and a minimal
// parsed struct (Runtime) for dedup.
type JiraComment struct {
    Runtime    JiraCommentRuntime
    Serialized map[string]any
}

type JiraCommentRuntime struct {
    ID       string `json:"id"`
    IssueKey string `json:"issueKey"`
    Created  string `json:"created"`
    Updated  string `json:"updated"`
}
```

The Runtime structs are intentionally minimal — only the fields needed
for dedup (`id`), cursor tracking (`fields.updated`), and file routing
(`key`, `issueKey`). Everything else lives in `Serialized` and
round-trips through disk unchanged.

The `fields.comment.comments` array is stripped from the Serialized
map before the issue line is written, so the issue snapshot never
double-stores comment bodies. The original `fields.comment.total`,
`maxResults`, and `startAt` metadata is preserved.

## Read Protocol Integration

Jira appears as a new source in the read protocol:

| Source | Data type | Platform |
|--------|-----------|----------|
| `jira` | Issues and comments | Jira |

### Selector

The selector is an issue key:

```
pigeon read jira ENG-101              # specific issue + comments
pigeon read jira                      # recent issue activity (all issues)
```

When no selector is given, the reader shows recently updated issues
(equivalent to `pigeon list --source=jira --since=7d`).

Selector matching is exact on the issue key (e.g. `ENG-101`) and fuzzy
on `fields.summary`. `pigeon read jira "deploy"` matches issues whose
summary contains "deploy".

### Filters

| Filter | Flag | Description |
|--------|------|-------------|
| Time window | `--since=DURATION` | Issues updated within duration |
| Specific date | `--date=YYYY-MM-DD` | Issues updated on a specific day |
| Status category | `--state=CATEGORY` | Filter by status category (`new`, `indeterminate`, `done`) |

Default when no filter: issues updated in the last 7 days.

### Read Algorithm

**Single issue** (`pigeon read jira ENG-101`):

1. Read `issues/ENG-101.jsonl`.
2. Deduplicate by `id` (keep last) — reduces to latest issue snapshot
   + all unique comments.
3. Display: issue header (key, summary, status, assignee, priority,
   components, labels), then description (converting ADF → markdown
   via `jira-cli`'s ADF renderer when available, or passing through as
   wiki markup on server installs), then comments in chronological
   order. `jsdPublic: false` comments are marked as "internal".

**All issues** (`pigeon read jira --since=7d`):

1. For each `issues/*.jsonl` file across all configured sites +
   projects, read the last issue line.
2. Filter by `fields.updated` within the requested time window.
3. Sort by `fields.updated` descending (most recently active first).
4. Display as a list: key, summary, status, assignee, last update.

### Context Integration

Jira projects appear in contexts alongside other platforms:

```yaml
contexts:
  work:
    gws: work@company.com
    slack: acme-corp
    linear: my-team
    jira:
      - site: acme
        project: ENG
      - site: acme
        project: OPS
```

A context may bind multiple Jira projects because projects on the same
site share auth but keep independent keyspaces.

## Greppability

Standard text tools work directly on the JSONL files:

```bash
# Find all issues and comments mentioning "deploy"
rg "deploy" ~/.local/share/pigeon/jira-issues/

# Find issues assigned to alice
rg '"displayName":"Alice' ~/.local/share/pigeon/jira-issues/acme/ENG/issues/

# Find all In Progress issues
rg '"name":"In Progress"' ~/.local/share/pigeon/jira-issues/acme/ENG/issues/

# Find comments by bob
rg '"type":"jira-comment".*"displayName":"Bob' ~/.local/share/pigeon/jira-issues/acme/ENG/issues/

# Count lines per issue (proxy for activity)
wc -l ~/.local/share/pigeon/jira-issues/acme/ENG/issues/*.jsonl

# Find all internal (non-public) comments
rg '"type":"jira-comment".*"jsdPublic":false' ~/.local/share/pigeon/jira-issues/
```

Because issues and comments are stored as raw CLI JSON, any field the
Jira REST API returns is grep-able. Adding a new query doesn't require
a code change — `jq` or `grep` on the stored lines is enough.

### ADF Caveat

On Jira Cloud, `fields.description` and comment `body` are ADF JSON
documents, not flat strings. A phrase that spans two text runs will
not grep as a single string:

```json
"content":[{"type":"text","text":"Fix login "},{"type":"text","marks":[{"type":"strong"}],"text":"timeout"}]
```

A grep for `"Fix login timeout"` will miss this line even though it
visually contains the phrase. For substring search against ADF
bodies, use `jq` to flatten:

```bash
jq -r 'select(.type=="jira-issue") | .fields.description
       | .. | .text? // empty' issues/ENG-142.jsonl | rg "deploy"
```

On Server installations (API v2), bodies are plain wiki-markup strings
and grep works directly. This is listed under Known Limitations.

### Pigeon Search Integration

The `pigeon grep` command includes Jira files in search globs:

```
pigeon grep "deploy" --source=jira --since=7d
```

## Maintenance

Issue files accumulate duplicate issue snapshots over time (one per
poll cycle where the issue changed). Maintenance compacts each file:

1. Deduplicate issue lines by `id` (keep last) — removes stale
   snapshots, keeping only the latest.
2. Deduplicate comment lines by `id` (keep last) — removes duplicates
   from re-fetches.
3. Rewrite the file with the deduplicated lines in chronological
   order.

Maintenance is lightweight because individual issue files are small
(tens to low hundreds of lines). It can run opportunistically without
blocking reads or writes.

## Configuration

Jira sites and projects are configured in `config.yaml`:

```yaml
jira:
  - site: acme                          # slug derived from the server URL
    server: https://acme.atlassian.net  # full Jira site URL
    project: ENG                        # project key
    config_file: ~/.config/pigeon/jira/acme-eng.yml   # path to the jira-cli config
    account: acme-eng                   # display name for pigeon
  - site: acme
    server: https://acme.atlassian.net
    project: OPS
    config_file: ~/.config/pigeon/jira/acme-ops.yml
    account: acme-ops
```

The `config_file` field points at a `jira-cli` config generated via
`jira init`. Pigeon invokes the CLI with `JIRA_CONFIG_FILE=<path>` set
in the environment, which is the supported way to multiplex multiple
Jira projects from one host. Auth tokens live where `jira-cli` puts
them (env `JIRA_API_TOKEN`, `.netrc`, or the system keyring) and are
never read by pigeon directly.

The `account` field is the display name shown by `pigeon list` and used
for directory naming scoping (it composes with `site` and `project` in
the directory layout only as metadata — on disk the path is always
`{site}/{project}/`).

## Known Limitations

- **No real-time events.** Jira has webhooks but the CLI wrapper is
  poll-based. Updates are delayed by at most one poll interval (30s).
- **ADF is not greppable as plain text.** On Jira Cloud, comment and
  description bodies are stored as ADF JSON. Substring search across
  text runs requires `jq` to flatten the document. Server
  installations (API v2) return plain strings and are unaffected.
- **Per-call pagination caps at 100.** Unlike the Linear CLI's
  `--limit=0`, `jira issue list` requires the poller to loop
  `--paginate <from>:100` pages. Large batches (first backfill on a
  long-lived project) cost many HTTP round-trips.
- **Comments past ~1,000 per issue are truncated.** `jira issue view`
  returns at most the server's default comment `maxResults` (1,000 on
  Cloud). Issues with more comments will lose older ones. Fixing this
  would require a separate `/rest/api/3/issue/{key}/comment`
  pagination loop, which the CLI does not expose.
- **JQL date precision is one minute.** The `updated > "<cursor>"`
  filter is minute-granular. The poller may re-fetch issues whose
  `updated` is within the overlap minute; dedup-on-ID absorbs the
  duplicate.
- **Edits between polls are collapsed.** If a comment is edited twice
  between two polls, only the final state is captured. Acceptable —
  the latest state is what matters.
- **No attachment download.** Issue attachments are stored as URLs in
  `fields.attachment[]`. The binary files are not downloaded in V1.
- **No changelog.** The history of status / assignee / field changes
  (`/rest/api/3/issue/{key}?expand=changelog`) is not fetched. `jira`
  CLI does not expose `expand=changelog` to callers. Adding it would
  require either an upstream CLI change or a direct HTTP call, both
  out of scope for V1.
- **Custom fields land under opaque `customfield_XXXXX` keys.** The
  raw API response does not resolve custom field display names. This
  is preserved verbatim on disk; readable names require the CLI's
  `jira issue view` (non-raw) rendering or a separate `/field`
  lookup.
- **Sprints, epics, and boards are out of scope for V1.** The CLI
  supports them (`jira sprint list`, `jira epic list`, `jira board
  list`), but they have no natural incremental cursor and their
  data model overlaps with regular issues (an epic *is* an issue of
  type `Epic`). Epic membership is already visible via `fields.parent`
  on child issues. Can be added later following the same raw-storage
  pattern.
- **Worklogs are out of scope for V1.** Same reasoning — `jira issue
  worklog` is a separate endpoint, incremental via `since=<epoch-ms>`
  on `/rest/api/3/worklog/updated`, and can be added later.
- **429 rate-limit handling is coarse.** The CLI surfaces 429 as a
  generic non-zero exit. Pigeon logs and retries on the next tick; no
  exponential backoff or `Retry-After` honoring. If this becomes a
  problem, a small wrapper around CLI invocations can parse stderr and
  sleep appropriately.
