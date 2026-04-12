# Identity Protocol

Pigeon stores cross-source person identities as plain text files,
greppable with standard tools. An identity maps a person to all their
known identifiers across platforms — email addresses, Slack user IDs
and mention names, WhatsApp phone numbers — so that a single query
like `--from=alice` resolves to the right identifier in every source.

Identity is stored **per source** at write time and **merged across
sources** at read time. Each listener or poller writes to its own account's
identity file; a read component loads the relevant files and merges them
in memory using stable identifiers (email, Slack ID, phone).

Contexts (see `read-protocol.md`) scope which accounts' files are merged.
When a context is active, reads only see identities from that context's
accounts. When no context is active, reads merge every known source.

## Storage Philosophy

Split writes, merged reads:

- **Write path**: each service owns its own `people.jsonl`. A Slack
  listener writing about a user never touches the GWS or WhatsApp files.
  Within a single source, signals still merge on stable identifier, so the
  per-source file stays deduplicated.
- **Read path**: a reader loads all the per-source files, merges them in
  memory via the same stable-identifier matching (email, Slack ID,
  phone), and returns a unified view.

One JSONL line per person, all identifiers on that line. This means a
single grep on any identifier — a name, an email, a Slack user ID, a
phone number — returns the complete (per-source) person in one hit. No
joins, no cross-file references for querying a single file.

Per-source files are small (hundreds to low thousands of lines) and
rewritten on update, not append-only. The per-source writer loads its
file into memory, merges new signals as they arrive, and rewrites
atomically (temp file + rename).

## Directory Layout

```
~/.local/share/pigeon/
├── identity/
│   ├── slack/
│   │   ├── acme-corp/
│   │   │   └── people.jsonl          # signals from the acme-corp workspace
│   │   └── vendor-ws/
│   │       └── people.jsonl
│   ├── gws/
│   │   └── alice-at-company-com/
│   │       └── people.jsonl          # signals from Gmail/Calendar/Drive
│   └── whatsapp/
│       └── 15551234567/
│           └── people.jsonl          # signals from the WhatsApp contact book
├── slack/
│   └── ...
├── whatsapp/
│   └── ...
└── gws/
    └── ...
```

Each `identity/<platform>/<account-slug>/` directory corresponds to one
configured account. Files are created on first write.

When no context is active, **all** identity files are merged — the reader
discovers every `identity/*/*/people.jsonl` under the data root. When a
context is active, only the accounts listed in that context are merged.

## Line Format

Each line is a JSON object representing one person:

```json
{"name":"Alice Smith","email":["alice@company.com"],"slack":{"acme-corp":{"id":"U04ABCDEF","mention":"Alice Smith"}},"seen":"2026-04-11"}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Best-known display name. Updated when a fresher signal arrives. |
| `email` | string[] | No | All known email addresses for this person. Primary join key across GWS and Slack. |
| `slack` | map[workspace → object] | No | Slack identities keyed by workspace slug. |
| `slack.{ws}.id` | string | Yes (if slack present) | Slack user ID (`U`-prefixed). Stable platform identifier. |
| `slack.{ws}.mention` | string | Yes (if slack present) | Display name as it appears after `@` in stored message text. Used for text-based mention search. |
| `whatsapp` | string[] | No | Phone numbers in E.164 format (e.g. `+15551234567`). |
| `seen` | string | Yes | Date of last observed activity (`YYYY-MM-DD`). For staleness detection. |

At least one of `email`, `slack`, or `whatsapp` must be present. A line
with only a `name` and `seen` date is not useful for resolution and
should not be written.

### Slack Mention Field

When a Slack message is stored, the resolver converts raw mentions from
`<@U04ABCDEF>` to `@DisplayName` in the message text. The `mention`
field records this display name so the resolution algorithm knows what
string to search for in message text.

The `mention` field reflects the display name at last observation. If a
user changes their Slack display name, the field updates on the next
observation. Old messages retain the old mention text — this is
expected. Text-based mention search is best-effort for historical data.

### Example Lines

A person known across Slack and GWS (email is the join key):

```json
{"name":"Alice Smith","email":["alice@company.com"],"slack":{"acme-corp":{"id":"U04ABCDEF","mention":"Alice Smith"}},"seen":"2026-04-11"}
```

A person known only in Slack (no GWS signals yet):

```json
{"name":"Bob Jones","slack":{"acme-corp":{"id":"U05BCDEFG","mention":"bob.jones"}},"seen":"2026-04-10"}
```

A person known across multiple Slack workspaces in the same context:

```json
{"name":"Carol Davis","email":["carol@company.com"],"slack":{"acme-corp":{"id":"U06CDEFGH","mention":"Carol Davis"},"vendor-ws":{"id":"U09XYZABC","mention":"Carol (Acme)"}},"seen":"2026-04-11"}
```

A person known only via WhatsApp:

```json
{"name":"Dave","whatsapp":["+15559876543"],"seen":"2026-04-09"}
```

A person known across all platforms:

```json
{"name":"Eve Park","email":["eve@company.com"],"slack":{"acme-corp":{"id":"U07DEFGHI","mention":"Eve Park"}},"whatsapp":["+15551234567"],"seen":"2026-04-11"}
```

## Greppability

One line per person with all identifiers means every grep — forward or
reverse — returns the complete identity:

```bash
# Forward: find all of alice's identifiers
rg "alice" identity/work/people.jsonl

# Reverse: who is Slack user U04ABCDEF?
rg "U04ABCDEF" identity/work/people.jsonl

# Reverse: who has this email?
rg "alice@company.com" identity/work/people.jsonl

# Reverse: who has this phone?
rg "15551234567" identity/work/people.jsonl

# List everyone with a Slack identity
rg '"slack"' identity/work/people.jsonl

# List everyone seen this week
rg '"seen":"2026-04-1' identity/work/people.jsonl
```

## Discovery

Identity signals are discovered **online** — as data flows through the
system, not by scanning files after the fact. Each listener and poller
pushes signals to the identity service as it encounters people.

### Signal Sources

**Slack listener startup** (richest source — connects user IDs to
emails):

The Slack API's `GetUsersContext()` returns full user profiles including
user ID, display name, real name, and email. On each listener startup
or workspace sync, every user profile is pushed as a signal:

```
Signal{Email: "alice@company.com", Name: "Alice Smith", Slack: {Workspace: "acme-corp", ID: "U04ABCDEF", Mention: "Alice Smith"}}
```

This is the primary source that connects Slack user IDs to email
addresses, enabling cross-source resolution with GWS.

**Gmail poller** (on each new email):

```
Signal{Email: "alice@company.com", Name: "Alice Smith"}
```

Pushed for the `from` field of each new email. The `to` and `cc` fields
are also pushed as signals (name may be absent for recipients).

**Calendar poller** (on each event with attendees):

```
Signal{Email: "alice@company.com", Name: "Alice Smith"}
```

Pushed for each attendee. The `displayName` field is not always present
in the Google Calendar API — when absent, only the email is pushed.

**Drive poller** (on each comment):

```
Signal{Email: "alice@company.com", Name: "Alice Smith"}
```

Pushed for comment authors and reply authors.

**WhatsApp listener** (on contact discovery):

```
Signal{Phone: "+15551234567", Name: "Dave"}
```

Pushed when contacts are loaded from the whatsmeow store.

### Signal Interface

The identity service exposes a single method for receiving signals:

```
Observe(signal) → error
```

All listeners and pollers call `Observe` when they encounter a person.
The identity service handles matching and merging internally.

## Merge Rules

Merging happens in two places with the same rules applied at different
times:

- **Write-time (per-source)**: when a signal arrives for a particular
  source, the per-source writer matches it against that source's own
  people file.
- **Read-time (cross-source)**: when a read is requested, the reader
  loads every relevant per-source file and merges them using the same
  matching rules.

### Matching

Signals (or persons, at read time) are matched to existing entries by
**stable identifiers only**, in this order:

1. **Email match**: signal's email matches any email in an existing
   person's `email` array.
2. **Slack user ID match**: signal's Slack user ID matches an existing
   person's `slack.{workspace}.id`.
3. **WhatsApp phone match**: signal's phone matches an existing
   person's `whatsapp` array.

Name is **never** used as a match key. Names are ambiguous, change over
time, and vary across platforms. Names are updated on match but never
used to establish one.

### Merge Behavior

**Match found — merge into existing person:**

- `name`: updated if the signal's source is higher priority (Slack
  profile > Gmail fromName > Calendar attendee displayName). The
  most specific name wins.
- `email`: appended if not already present.
- `slack`: workspace entry added or updated (ID and mention).
- `whatsapp`: phone appended if not already present.
- `seen`: updated to today if newer.

**No match found — create new person:**

A new line is appended with whatever identifiers the signal provides.
The person may be merged later if a future signal provides a connecting
identifier (e.g. a Slack profile sync reveals the email matches an
existing email-only person from Gmail).

### What Is Not Merged

- **Cross-context merging**: never. Each context's identity file is
  independent. The same person may exist in multiple context files
  with different identifiers.
- **Name-based merging**: never. Two people named "Alice" in different
  sources remain separate until a shared email or platform ID connects
  them.
- **Forced merging**: no heuristics, no fuzzy matching on identifiers.
  Merging happens only on exact identifier match.

## Manual Editing

The identity file is plain text and can be edited directly. Use cases:

- **Link identifiers**: add a WhatsApp phone to an existing person who
  was only known via Slack/email.
- **Merge duplicates**: if the same person appears as two lines (e.g.
  one from Slack, one from Gmail, before the email link was
  discovered), manually combine them into one line and delete the
  other.
- **Correct names**: fix display names that were auto-discovered with
  wrong or partial values.
- **Remove people**: delete a line to exclude someone from resolution.

A CLI helper may be added in the future:

```
pigeon identity link "alice" --whatsapp=+15551234567
pigeon identity link "alice" --email=alice.personal@gmail.com
pigeon identity list
pigeon identity list --slack
```

## Resolution Algorithm

When a command includes a person filter (e.g. `--from=alice`):

1. **Load** the identity file for the active context.
2. **Match** against the `name` field using case-insensitive substring
   matching. Also match against email prefixes (the part before `@`)
   and Slack mention names.
3. **Disambiguate**: if exactly one person matches, use it. If multiple
   match, return an error listing the candidates with their
   identifiers. If none match, return an error.
4. **Extract** per-source identifiers for the accounts in the active
   context.
5. **Apply** as filters in the read/search layer:

| Source | Filter by |
|--------|-----------|
| Gmail | `from` field ∈ person's `email` array |
| Calendar | `attendees[].email` ∈ person's `email` array |
| Drive comments | `author.emailAddress` ∈ person's `email` array |
| Slack | `from` field = person's `slack.{workspace}.id` |
| Slack (text mentions) | message `text` contains `@` + person's `slack.{workspace}.mention` |
| WhatsApp | `from` field matches person's `whatsapp` JID |

## File Lifecycle

1. **Creation**: the identity file is created when the first signal
   arrives for a context. Typically this happens on Slack listener
   startup (bulk user profile load).

2. **Updates**: the identity service rewrites the file after each batch
   of signals (e.g. after processing all users from a Slack sync, not
   after each individual user). Writes are atomic (write to temp file,
   rename).

3. **Staleness**: the `seen` field tracks when a person was last
   observed. People not seen for an extended period may have left the
   organization. No automatic pruning — staleness is informational.

4. **Deletion**: removing a context from config does not delete its
   identity file. The file remains on disk and can be reused if the
   context is recreated.

## Known Limitations

- **Slack display name changes**: if a user changes their Slack display
  name, old messages retain the old `@mention` text. The identity file
  updates to the new name on next observation, but text-based mention
  search for historical messages uses the old name. This is inherent
  to how Slack mentions are stored.

- **GWS-only people have no Slack ID**: a person known only from Gmail
  or Calendar (e.g. an external contact) has email but no Slack user
  ID. They can be found in Gmail/Calendar/Drive but not in Slack
  messages.

- **WhatsApp names are unstable**: WhatsApp push names are set by the
  contact and can change at any time. The `name` field may not match
  the name shown in older messages.

- **No profile photos or rich metadata**: the identity file stores
  identifiers for resolution, not a full contact profile. Rich
  metadata (profile photos, job titles, timezones) is out of scope.

- **Bot identities**: Slack bots have bot IDs (`B`-prefixed) rather
  than user IDs (`U`-prefixed). Bots are stored as regular people in
  the identity file. The identity service treats them the same as
  human users.
