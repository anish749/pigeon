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
Calendar events, and Drive comments: each JSONL line is the **raw HTTP
response body** from the Jira REST API plus a single injected `type`
discriminator. Pigeon does not pick a subset of "interesting" fields —
the full JSON response lands on disk verbatim. This makes storage
lossless against any field we don't currently use and future-proof
against schema changes in the Jira REST API.

Pigeon imports
[ankitpokhrel/jira-cli](https://github.com/ankitpokhrel/jira-cli)'s
`pkg/jira` package directly as a Go library — no subprocess. Two
client methods cover the entire ingest path:

- `client.Search(jql, limit)` (v3, `/search/jql?fields=*all`) or
  `client.SearchV2(jql, from, limit)` (v2, offset paging) — used to
  discover issue keys whose `updated` is past the cursor.
- `client.GetIssueRaw(key)` / `client.GetIssueV2Raw(key)` — returns
  the raw HTTP response body for `GET /rest/api/3/issue/{key}` (or v2)
  as a `string`. This is the lossless byte-for-byte payload pigeon
  writes to disk.

The `jira` CLI binary is still required, but only as a peer tool: the
user runs `jira init` to generate the credential config that pigeon
reads, and any agent-driven write actions (commenting, transitioning,
creating issues) go through the CLI directly. Pigeon never invokes
the binary at runtime.

`pkg/jira` owns authentication wiring, cloud-vs-server API version
selection (v2/v3), and the Atlassian Document Format / markdown
round-trip. Pigeon owns cursor management, JQL composition, and
pagination loops (see "Polling and Sync" below — `pkg/jira`'s `Search`
function does not internally page v3 token-based responses, so the
poller drives that loop).

### Why `GetIssueRaw`, not `Search` results

`pkg/jira` exposes two ways to obtain issue data, and they are not
equivalent:

- `client.GetIssueRaw(key)` returns the **raw HTTP response body** from
  `GET /rest/api/3/issue/{key}` as a string. Every field the Jira API
  returns — including ones `pkg/jira`'s typed structs do not model —
  is preserved verbatim. Comments live inside this response at
  `fields.comment.comments[]`.
- `client.Search(jql, limit)` returns `*SearchResult` whose `Issues`
  field is `[]*jira.Issue` — a **trimmed, parsed struct** declared in
  [`pkg/jira/types.go`](https://github.com/ankitpokhrel/jira-cli/blob/main/pkg/jira/types.go).
  Many fields (attachments, custom fields, work log, votes, changelog,
  worklog, etc.) are dropped because the `IssueFields` Go type doesn't
  define them; they are present in the HTTP body but never reach the
  caller.

For lossless on-disk storage, pigeon **always writes the
`GetIssueRaw` body**, never the `Search` payload. The search call is
only used to discover which issue keys changed; the per-issue raw
fetch is the source of truth.

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

`pkg/jira` is HTTP-only, so all sync is poll-based.

| Data type | Client call | Cursor |
|-----------|-------------|--------|
| Issue keys | `client.Search(jql, limit)` (v3) or `client.SearchV2(jql, from, limit)` (v2), where `jql = "updated > '<cursor>' AND project in (...)"`. The poller iterates through `Issues[]` and only reads each `*jira.Issue.Key`. | `updated` timestamp |
| Issue body + comments | `client.GetIssueRaw(key)` (v3) or `client.GetIssueV2Raw(key)` (v2) | Per-issue, fetched when the issue key appears in the search results |

Two API calls per changed issue: one search (discovery) + one raw
fetch per key. This mirrors how the Linear poller uses `issue query` +
`issue view`.

### Why Two Calls Per Issue

`Search` already returns the parsed issues in one HTTP round-trip,
but — as noted under "Why `GetIssueRaw`" above — `*jira.Issue` is
the trimmed Go view, not the raw HTTP body. Writing the trimmed form
to disk would silently drop every field `IssueFields` does not model.
Issuing one extra `GetIssueRaw` per changed issue is the cost of
lossless storage.

For the discovery step we therefore care only about the `Key` field on
each returned `*jira.Issue`; the rest of the parsed payload is
discarded.

### First-Run Backfill

Jira issues, like Linear issues, are mutable entities. The backfill
fetches two JQL sets, one project at a time:

1. **All active issues** (any status category except Done):

   ```
   project = "<KEY>" AND statusCategory != Done
   ```

2. **Recently closed issues** (Done status category, updated in the
   last 90 days):

   ```
   project = "<KEY>" AND statusCategory = Done AND updated > -90d
   ```

Each JQL is passed to `client.Search` (v3) or `client.SearchV2` (v2)
and paginated until exhausted (see "Pagination" below). The poller
collects the `Key` from each returned `*jira.Issue`, then for each key
calls `client.GetIssueRaw(key)` and writes:

- One issue line (the top-level response with `fields.comment.comments`
  stripped).
- One comment line per entry that was originally in
  `fields.comment.comments[]`.

The 90-day window for closed issues matches the backfill depth used by
other pigeon sources.

#### Pagination

`pkg/jira` paginates differently across versions:

- **v3**: `client.Search` calls `/search/jql` once and returns
  `SearchResult{IsLast bool, NextPageToken string, Issues []*Issue}`.
  The function does **not** accept a token, so the poller cannot pass
  `nextPageToken` back through `Search`. To continue past the first
  page, the poller calls `client.Get(ctx, "/search/jql?jql=...&nextPageToken=...&fields=*all", nil)`
  directly until `IsLast` is true.
- **v2**: `client.SearchV2(jql, from, limit)` accepts an offset, so
  the poller loops with `from = 0, limit, 2*limit, ...` until an empty
  page returns.

A reasonable `limit` is 100. The `/search/jql` endpoint enforces a
hard range of `[1, 5000]` on the `maxResults` query parameter — calls
with `limit=0` return HTTP 400 (verified against Jira Cloud, April
2026). The poller must never pass `0` even when "no results expected"
is the intent.

### Incremental Sync

Each poll cycle:

1. Load cursor (last `updated` timestamp) from `.sync-cursors.yaml`.
2. Build JQL: `updated > "<cursor>" AND project in (PROJ1, PROJ2, ...)`
   (or one project per cycle, if the poller iterates per project).
3. Call `client.Search(jql, 100)` (v3) or `client.SearchV2(jql, 0, 100)`
   (v2), looping pages until exhausted (see "Pagination" above).
4. For each returned `*jira.Issue`, take its `.Key`, then:
   a. Call `client.GetIssueRaw(key)` (v3) or `client.GetIssueV2Raw(key)` (v2).
   b. Append the issue snapshot to `issues/{KEY}/issue.jsonl` (with
      `fields.comment.comments` removed from the line).
   c. Append each comment as a separate `jira-comment` line to
      `issues/{KEY}/comments.jsonl`.
5. Update the cursor to the maximum `fields.updated` across all issues
   fetched in this batch.
6. Save the cursor.

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

> **⚠ Silent parse failure.** Verified against Jira Cloud
> (April 2026): JQL does **not** raise an error for unparseable date
> strings. Any malformed cutoff — RFC 3339, ISO 8601 with `Z`, junk
> like `"banana"` — is silently treated as never-true, causing the
> query to return zero matches. The poller would then believe nothing
> has changed, fail to advance the cursor, and continue returning
> empty results indefinitely with no error in the logs. Implementations
> must:
>
> 1. Format the cursor to one of the four accepted JQL forms before
>    interpolation (the poller already does this — never pass through
>    the stored RFC 3339 form).
> 2. Validate at boot that a known-good probe (e.g. `updated > "2010-01-01"`)
>    returns a non-zero count for at least one configured project. A
>    zero count there is the canary for a silent format regression.

JQL date filters are minute-precision. The poller rounds the cursor
*down* to the minute when formatting for JQL, which means a single
overlap minute may be re-fetched on the next poll. Dedup-on-ID handles
this without harm.

### Comment Fetching

Comments are fetched as part of `client.GetIssueRaw`, which returns
the full issue body. The poller only calls `GetIssueRaw` for keys
that appeared in the incremental search (i.e. issues that changed
since the last poll).

Comments are nested under `fields.comment.comments[]` in the response.
Each comment has an `id`, and the dedup rule (keep last by ID) handles
re-appends of already-seen comments.

The `fields.comment` object also carries `total`, `startAt`, and
`maxResults`. On a very active issue, only the most recent N comments
(default 1,000 on Jira Cloud) come back in the issue body. Pigeon
does not paginate comments separately — if a single issue has more
than 1,000 comments, older ones will be missed. `pkg/jira` does not
expose a paginated comment endpoint either, so a future fix would
need a direct call to `/rest/api/3/issue/{key}/comment`. This is
listed under Known Limitations.

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

If the client returns an error wrapping HTTP 429 (surfaced via
`*jira.ErrUnexpectedResponse` from `pkg/jira` with `Status` set to
`429 Too Many Requests`), the poller logs an error, skips the cursor
update, and retries on the next tick. `pkg/jira` does not implement
backoff or `Retry-After` honoring, so pigeon treats 429s as transient
errors and relies on the 30-second tick to space out retries.

### Cursor Expiry

The JQL `updated` cursor is a plain date string, not an opaque server
token. It does not expire. If the cursor is very old (weeks), the
first incremental poll may return a large batch spread across many
pages. This is handled the same as backfill — process all pages,
dedup on disk.

## Directory Layout

```
~/.local/share/pigeon/
└── jira/                                  # platform
    └── {account-slug}/                    # first DNS label of the server URL
        └── {project-key}/                 # e.g. ENG (case preserved from project.key)
            ├── .sync-cursors.yaml         # cursor state
            └── issues/
                ├── ENG-101/
                │   ├── issue.jsonl        # snapshots
                │   └── comments.jsonl     # comments
                ├── ENG-142/
                │   ├── issue.jsonl
                │   └── comments.jsonl
                └── ENG-205/
                    ├── issue.jsonl
                    └── comments.jsonl
```

`{account-slug}` is the lowercased first DNS label of the `server`
field in the bound `jira-cli` YAML — `https://acme.atlassian.net`
becomes `acme`. Pigeon does not ask the user to invent a slug because
`jira-cli` itself has no slug concept and there is no canonical
identifier in its configuration; the URL hostname is the only stable
naming the user already controls. Two `jira-cli` configs with the
same server hostname collide on disk; pigeon treats this as the user
having two pointers at the same instance, which is the intended
semantic.

`{project-key}` is the value of `project.key` from the bound
`jira-cli` YAML — `jira init` writes one default project per config
file and pigeon ingests that one. Pigeon does not invent multi-project
semantics on top of `jira-cli`; for a second project, the user runs
`jira init` again with a different config path and lists both paths
in pigeon's `config.yaml`.

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
prefixes (`ENG-`, `OPS-`, `MKT-`, …). `jira-cli` is configured against
a single site + single default project per config file, switched via
`JIRA_CONFIG_FILE` or the `-c/--config` flag.

Pigeon follows that one-config-one-project model exactly. Each pigeon
`jira:` entry binds one `jira-cli` YAML; the resulting on-disk path
is `{account-slug}/{project-key}` where both segments are read from
that YAML. Multi-project on the same site means multiple `jira-cli`
configs, each pointing at the same server with a different `project.key`,
each listed as its own entry in pigeon's `config.yaml`. Multi-site
works the same way — different `server`, different `jira-cli` YAML,
another entry.

### Cursor File

Path: `jira/{account-slug}/{project-key}/.sync-cursors.yaml`

```yaml
issues:
  updated_after: "2026-04-11T14:30:00.000Z"
```

A single cursor per project. The timestamp is the maximum `updated`
seen across all issues in the last successful poll, stored in RFC 3339
and rounded down to minute precision when formatted into JQL.

## Line Types

### Issue Line

Each line is the **raw `client.GetIssueRaw(key)` JSON for one issue**
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
`GetIssueRaw` response, plus a `"type":"jira-comment"` discriminator
and the parent issue key for grep-ability. Everything else is
verbatim from the API response.

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

Jira types follow the same dual-representation pattern as
`CalendarEvent` (`internal/store/modelv1/gws_event.go`): a typed
Runtime view for in-process code (dedup, cursor extraction, file
routing) and a `Serialized map[string]any` that is the source of
truth for disk storage.

```go
import jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

// JiraIssue holds the raw HTTP response (Serialized) and the typed
// pkg/jira.Issue (Runtime) for dedup, cursor extraction, and file routing.
type JiraIssue struct {
    Runtime    jira.Issue       // pkg/jira's typed Issue (Key + IssueFields)
    Serialized map[string]any   // raw HTTP body from GetIssueRaw, source of truth on disk
}
```

For issues, `pkg/jira.Issue` already carries every field pigeon needs:
`Key` (file routing), `Fields.Updated` (cursor), and `Fields.Created`,
`Fields.Status`, `Fields.Assignee`, etc. (formatting). The numeric
`id` used as the dedup key is **not** modeled by `pkg/jira.Issue`;
it lives in the raw response. Pigeon extracts it from the
`Serialized` map (`Serialized["id"].(string)`) at write time and uses
that for the dedup index.

```go
// JiraComment is a single entry from fields.comment.comments[]. pkg/jira
// does not expose a named comment type — IssueFields.Comment.Comments
// is an anonymous struct (see pkg/jira/types.go:105-113) — so the
// Runtime here is a small local type defined by pigeon.
type JiraComment struct {
    Runtime    JiraCommentRuntime
    Serialized map[string]any   // raw fields.comment.comments[i] JSON, source of truth
}

type JiraCommentRuntime struct {
    ID       string `json:"id"`
    IssueKey string `json:"issueKey"` // injected by pigeon, not in raw API output
    Created  string `json:"created"`
    Updated  string `json:"updated"`  // present in raw API output but not in pkg/jira.Issue.Fields.Comment
}
```

`JiraCommentRuntime` is intentionally minimal — only the fields needed
for dedup (`id`), filtering by parent (`issueKey`), and ordering
(`created`, `updated`). All other fields (`author`, `body`, `self`,
`jsdPublic`, …) live in `Serialized` and round-trip unchanged.

### Building Serialized

Both Runtime and Serialized are populated from the same raw JSON
returned by `client.GetIssueRaw`. The poller does:

1. `raw, err := client.GetIssueRaw(key)` → `string` of the full body.
2. `json.Unmarshal([]byte(raw), &issue.Runtime)` to populate
   `jira.Issue` (typed view, missing fields are tolerated).
3. `json.Unmarshal([]byte(raw), &issue.Serialized)` to populate the
   raw map (lossless).
4. Lift `comments := issue.Serialized["fields"]["comment"]["comments"]`
   into a separate slice, then delete that nested key. The mutated
   `Serialized` becomes the issue line; each lifted comment becomes a
   `JiraComment` (with `IssueKey` injected) and writes a comment line.

The original `fields.comment.total`, `maxResults`, and `startAt` keys
are preserved — only the `comments` array itself is stripped, so
issue lines never double-store comment bodies.

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
rg "deploy" ~/.local/share/pigeon/jira/

# Find issues assigned to alice (issue snapshots only)
rg '"displayName":"Alice' ~/.local/share/pigeon/jira/acme/ENG/issues/*/issue.jsonl

# Find all In Progress issues
rg '"name":"In Progress"' ~/.local/share/pigeon/jira/acme/ENG/issues/*/issue.jsonl

# Find comments by bob
rg '"displayName":"Bob' ~/.local/share/pigeon/jira/acme/ENG/issues/*/comments.jsonl

# Count comments per issue
wc -l ~/.local/share/pigeon/jira/acme/ENG/issues/*/comments.jsonl

# Find all internal (non-public) comments
rg '"jsdPublic":false' ~/.local/share/pigeon/jira/*/issues/*/comments.jsonl
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

## Setup

Pigeon piggybacks on the user's existing `jira-cli` setup rather than
maintaining a parallel auth/config story. The user must have
`jira-cli` installed and initialized before pigeon can ingest Jira.

1. **Install `jira-cli`.** Homebrew, downloadable releases, Nix, or
   Docker — see the
   [installation guide](https://github.com/ankitpokhrel/jira-cli/wiki/Installation).
2. **Create an Atlassian API token** at
   `https://id.atlassian.com/manage-profile/security/api-tokens`
   (Cloud) or generate a Personal Access Token (Server / on-prem).
   `jira init` does not create tokens — it consumes one. See "SSO and
   the API Token Gotcha" below before doing this if your Jira sits
   behind Okta / SAML / Google SSO.
3. **Export the token**: `export JIRA_API_TOKEN=<token>`. For Server
   PATs, also `export JIRA_AUTH_TYPE=bearer`.
4. **Pre-flight check** (recommended — faster than retrying `jira init`
   if something is off):

   ```sh
   curl -s -u "<your-atlassian-email>:$JIRA_API_TOKEN" \
     https://<your-site>.atlassian.net/rest/api/3/myself | head -c 400
   ```

   - 200 + JSON with `accountId`, `emailAddress`, `displayName` → token
     and email are correct, proceed.
   - 401 → token / email mismatch (re-read the SSO section below).
   - 404 → server URL typo.
5. **Initialize `jira-cli`**: `jira init` walks the user through
   `installation` (cloud / local), `server`, `login`, and a default
   `project`. It calls live API endpoints (`/project`, `/field`,
   `/board`) under the exported token to populate the YAML, so step 2
   must come first. For Cloud, **always pick `auth_type: basic`** —
   `bearer` is for Server PATs only, even when your Cloud login goes
   through SSO.
6. **Verify**: `jira me` should print the authenticated user.
7. **Add a pigeon config entry** (see "Configuration" below). For a
   single-config user with `JIRA_CONFIG_FILE` unset, this is one line
   (`- {}`) — the empty entry resolves to `jira-cli`'s default path.

If the user already runs `jira-cli` for daily work, only step 7 is new.
The poller will pick up the entry on the daemon's next config-watch
tick and start backfilling the bound project.

### SSO and the API Token Gotcha

Atlassian Cloud sites at `*.atlassian.net` are usually fronted by
Okta, Google, or another SAML/SSO provider for browser login. **The
SSO password is never the API credential.** SSO governs the browser
session; REST API calls authenticate with HTTP Basic using
`<email>:<api_token>`, where `<api_token>` is generated at
`https://id.atlassian.com/manage-profile/security/api-tokens`.

Three failure modes that all surface as `401 unauthorized` in
`jira init`:

- **Exported the SSO password as `JIRA_API_TOKEN`.** Will always 401.
  Generate a real API token at the link above. The token page is
  reachable even when your org enforces SSO — it's an
  Atlassian-account-level setting, not an org SSO setting.
- **Wrong email for `login`.** The login must be the email tied to the
  Atlassian account that created the token, not necessarily your
  corporate email. They are usually the same, but if your Atlassian
  account was set up with a personal email, the corporate one will
  401.
- **Picked `bearer` as `auth_type`.** Bearer is for self-hosted Jira
  Server / Data Center with PATs. Cloud always uses `basic` even with
  SSO.

The pre-flight `curl` in step 4 catches all three before you waste a
round of `jira init`.

## Configuration

Pigeon's only Jira-specific config is the path to a `jira-cli` YAML.
Server, login, auth, project, and everything else live in that YAML
and are read at runtime; pigeon never duplicates them.

```yaml
jira:
  - jira_config: ~/.config/.jira/.config.yml   # optional; defaults to $JIRA_CONFIG_FILE or jira-cli's default path
```

For the most common case — a user with one `jira init` already done
and `JIRA_CONFIG_FILE` unset — the entry is a one-liner with no fields
at all:

```yaml
jira:
  - {}
```

### Field semantics

- **`jira_config`** (string, optional): Path to a `jira-cli` YAML.
  Resolution order: explicit value here → `$JIRA_CONFIG_FILE` env →
  `jira-cli`'s default (`~/.jira/.config.yml`). Set this when you
  maintain multiple `jira-cli` configs (one per Atlassian site or one
  per project).

### Multiple projects or sites

Each pigeon entry binds one `jira-cli` YAML, which is one project.
For a second project — same site or different — generate a second
`jira-cli` config with `JIRA_CONFIG_FILE=<new-path> jira init` and
add the new path as another pigeon entry:

```yaml
jira:
  - jira_config: ~/.jira/acme-eng.yml      # project ENG on acme.atlassian.net
  - jira_config: ~/.jira/acme-ops.yml      # project OPS on acme.atlassian.net
  - jira_config: ~/.jira/contoso-support.yml  # project SUPPORT on a different site
```

V1 assumes a single `JIRA_API_TOKEN` env var covers all sites. If a
user genuinely needs different tokens per site, multi-token routing is
deferred to a later version (per-entry `api_token_env` override is the
expected shape).

### What pigeon reads from the jira-cli YAML

Only the fields needed to construct a `jira.Config` for `pkg/jira.NewClient`:

| jira-cli YAML key | Used as |
|---|---|
| `server` | `jira.Config.Server` (runtime only — not part of any on-disk path) |
| `login` | `jira.Config.Login` |
| `auth_type` | `jira.Config.AuthType` (`basic`, `bearer`, or `mtls`) |
| `insecure` | `jira.Config.Insecure` |
| `mtls.ca_cert`, `mtls.client_cert`, `mtls.client_key` | `jira.Config.MTLSConfig` (when `auth_type: mtls`) |
| `installation` | Selects v3 (cloud) vs v2 (local) endpoint methods |

Discovered fields written by `jira init` (`project.key`, `board`,
`epic.name`, `epic.link`, `issue.types[]`, `issue.fields.custom[]`)
are ignored. They exist for `jira-cli`'s write commands and do not
affect read-only ingest.

### Token sourcing

The API token is read once at `pigeon setup-jira` time from the
`JIRA_API_TOKEN` environment variable, verified end-to-end via
`client.Me()`, and persisted to pigeon's `config.yaml` as the
per-entry `api_token` field. After setup, the daemon reads the token
from the config; environment variables play no role at runtime.

`setup-jira` also captures the account name from the bound YAML's
`server` URL (first DNS label, lowercased) and persists it on the
entry as `account`, alongside `jira_config` and `api_token`. The
on-disk identifier is the slug of that name (`jira/{slug}/...`);
persisting the name lets the daemon and workspace machinery construct
the account without reopening the jira-cli YAML.

If a `jira:` entry is missing any of `jira_config`, `api_token`, or
`account`, the daemon refuses it and logs an error pointing at
`pigeon setup-jira`. Re-running `setup-jira` upserts the entry by
resolved path so it remains the canonical source of those fields.

Users who rely on `.netrc` or keyring with `jira-cli` need to
`export JIRA_API_TOKEN` once before running `setup-jira` so the value
can be lifted into pigeon's config.

## Known Limitations

- **No real-time events.** Jira has webhooks but `pkg/jira` is
  HTTP-only. Updates are delayed by at most one poll interval (30s).
- **ADF is not greppable as plain text.** On Jira Cloud, comment and
  description bodies are stored as ADF JSON. Substring search across
  text runs requires `jq` to flatten the document. Server
  installations (API v2) return plain strings and are unaffected.
- **`pkg/jira.Search` does not loop v3 token pagination.** The
  function calls `/search/jql` once and returns the first page plus a
  `NextPageToken`, but exposes no API to pass that token back. The
  poller must call `Client.Get` directly (with the same query plus
  `&nextPageToken=...`) to walk subsequent pages. V2 is unaffected
  (`SearchV2` accepts an offset).
- **Comments past ~1,000 per issue are truncated.** `GetIssueRaw`
  returns at most the server's default comment `maxResults` (1,000 on
  Cloud). Issues with more comments will lose older ones. Fixing this
  would require a separate `/rest/api/3/issue/{key}/comment`
  pagination loop, which `pkg/jira` does not expose.
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
  (`/rest/api/3/issue/{key}?expand=changelog`) is not fetched.
  `pkg/jira.GetIssueRaw` does not expose `expand=changelog`. Adding
  it would require an upstream addition to `pkg/jira` or a direct
  `Client.Get` call from pigeon — out of scope for V1.
- **Custom fields land under opaque `customfield_XXXXX` keys.** The
  raw API response does not resolve custom field display names. This
  is preserved verbatim on disk; readable names require a separate
  `/field` lookup (which `jira init` does and writes to its YAML, but
  pigeon does not currently consume the resolved mapping).
- **Sprints, epics, and boards are out of scope for V1.** `pkg/jira`
  exposes them (e.g. `Sprints()`, `Boards()`), but they have no
  natural incremental cursor and their data model overlaps with
  regular issues (an epic *is* an issue of type `Epic`). Epic
  membership is already visible via `fields.parent` on child issues.
  Can be added later following the same raw-storage pattern.
- **Worklogs are out of scope for V1.** `/rest/api/3/worklog/updated`
  is a separate endpoint with its own `since=<epoch-ms>` cursor. It
  can be added later.
- **429 rate-limit handling is coarse.** `pkg/jira` surfaces 429s
  via `*ErrUnexpectedResponse`. Pigeon logs and retries on the next
  tick; no exponential backoff or `Retry-After` honoring.
- **Single token across multiple sites.** V1 sources `JIRA_API_TOKEN`
  from a single env var. Users with multiple Atlassian sites that
  require different tokens are not supported until a per-entry
  `api_token_env` override is added.
- **`.netrc` and keyring auth not honored.** `pkg/jira.Config.APIToken`
  is a plain string. Pigeon populates it only from the
  `JIRA_API_TOKEN` env var. Users who configured `jira-cli` to read
  from `.netrc` or the OS keyring must export the env var separately
  for pigeon.
