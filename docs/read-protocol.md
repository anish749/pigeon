# Read Protocol

Pigeon's read layer provides structured access to stored data across all
platforms and services. This document defines the selection model, CLI
surface, and resolution rules for reading data.

The read layer is designed for AI agent consumption. Every command should
be unambiguous and self-documenting — an agent that sees `pigeon read
calendar --since=7d` should never end up in a nonsensical state.

## Selection Model

Every read operation has four orthogonal dimensions:

| Dimension | What it answers | Required |
|-----------|----------------|----------|
| **Context** | Which group of accounts? | No (default or inferred) |
| **Source** | What kind of data? | Yes (always explicit) |
| **Selector** | Which resource within the source? | Depends on source |
| **Filters** | What slice of the data? | No (defaults apply) |

These dimensions are independent. Context narrows the account set.
Source picks the data type. Selector picks a resource within that type.
Filters narrow the time range or quantity.

### Context

A context is a named set of accounts that forms a workspace boundary.
When a context is active, all pigeon commands — read, list, grep, glob,
send — operate only on accounts within that context.

Contexts are configured in `config.yaml`:

```yaml
contexts:
  work:
    gws: work@company.com
    slack: acme-corp
  personal:
    gws: user@gmail.com
    slack: side-project
    whatsapp: "+15551234567"
  project-x:
    gws: work@company.com       # same account can appear in multiple contexts
    slack: [acme-corp, vendor-ws]
    whatsapp: "+15551234567"     # same WhatsApp across contexts

default_context: personal
```

An account can appear in multiple contexts. A context can contain
multiple accounts of the same platform type (e.g. two Slack workspaces).

**Resolution order:**

1. `PIGEON_CONTEXT` environment variable
2. `--context` flag (per-command override)
3. `default_context` in config
4. No context — all accounts visible, explicit `-a` required when
   ambiguous

Setting a context is a soft restriction — pigeon can always access files
directly via `rg` or `jq` regardless of context. The context boundary
scopes pigeon's own commands only.

When no context is set and only one account exists for a given source
type, the account is inferred automatically. When multiple accounts
exist and no context or `-a` flag resolves the ambiguity, pigeon errors
with a message listing the available accounts.

### Source

The source is the type of data being read. It is always the first
positional argument to `pigeon read`:

| Source | Data type | Platform |
|--------|-----------|----------|
| `gmail` | Email messages | GWS |
| `calendar` | Calendar events | GWS |
| `drive` | Google Docs and Sheets | GWS |
| `slack` | Slack messages | Slack |
| `whatsapp` | WhatsApp messages | WhatsApp |

Source is always explicit — there is no flag that doubles as a source
selector. `pigeon read gmail` and `pigeon read calendar` are different
subcommands, not different values of a `--contact` flag.

**Context × Source → Account.** Within a context, the source type
resolves to the account(s) for that platform. If the context has one
GWS account, `gmail`, `calendar`, and `drive` all resolve to it. If the
context has two Slack workspaces, `slack` requires a selector or `-a` to
disambiguate.

### Selector

The selector identifies a specific resource within a source. Its meaning
and requirement depend on the source type:

| Source | Selector | Required | What it selects |
|--------|----------|----------|-----------------|
| `gmail` | — | No | One inbox per account |
| `calendar` | calendar ID | No (default: `primary`) | Which calendar |
| `drive` | document name | Yes | Which document or sheet |
| `slack` | channel or DM | Yes | `#channel`, `@user`, or group DM |
| `whatsapp` | conversation | Yes | Contact name, phone, or group |

The selector is a positional argument, not a flag:

```
pigeon read slack #engineering --since=2h
pigeon read whatsapp Alice --since=2h
pigeon read drive "Q2 Planning"
```

For sources that don't require a selector (`gmail`, `calendar`), the
default resource is the full inbox or the primary calendar. An optional
flag can override the default:

```
pigeon read calendar                        # primary calendar
pigeon read calendar --calendar=secondary   # named calendar
```

Selector matching is fuzzy. `pigeon read slack eng` matches
`#engineering` if it is the only channel containing "eng" in the
resolved account. Ambiguous matches produce an error listing candidates.

### Filters

Filters narrow the data within the selected resource. They are named
flags, shared across all source types where they apply:

| Filter | Flag | Applies to | Description |
|--------|------|------------|-------------|
| Time window | `--since=DURATION` | All | Items within duration from now (e.g. `30m`, `2h`, `7d`) |
| Specific date | `--date=YYYY-MM-DD` | All | Items from a specific day |
| Quantity | `--last=N` | gmail, slack, whatsapp | Last N items |

When no filter is specified, the default is source-dependent:

| Source | Default |
|--------|---------|
| `gmail` | Last 25 emails |
| `calendar` | Today's events |
| `drive` | Current document content + recent comments |
| `slack` | Today's messages |
| `whatsapp` | Today's messages |

---

## CLI Surface

### Read

```
pigeon read <source> [selector] [flags]
```

**Gmail:**

```
pigeon read gmail                          # last 25 emails (default)
pigeon read gmail --since=7d               # emails from last 7 days
pigeon read gmail --last=10                # last 10 emails
pigeon read gmail --date=2026-04-11        # emails from a specific day
```

**Calendar:**

```
pigeon read calendar                       # today's events
pigeon read calendar --since=7d            # this week's events
pigeon read calendar --date=2026-04-14     # events on a specific day
```

**Drive:**

```
pigeon read drive "Q2 Planning"            # document content + comments
pigeon read drive "Budget Sheet"           # sheet content + comments
```

**Slack:**

```
pigeon read slack #engineering --since=2h
pigeon read slack @alice --last=50
pigeon read slack #general --date=2026-04-10
```

**WhatsApp:**

```
pigeon read whatsapp Alice --since=2h
pigeon read whatsapp "Book Club" --last=20
```

**Cross-context override:**

```
pigeon read gmail --context=work --since=24h    # override active context
pigeon read calendar -a work@company.com        # bypass context entirely
```

### List

`pigeon list` respects the active context:

```
$ export PIGEON_CONTEXT=work
$ pigeon list
Sources for work:

  gmail                                    work@company.com
  calendar/primary                         work@company.com
  drive/q2-planning-FILEID                 "Q2 Planning" (Google Doc)
  drive/budget-FILEID                      "Budget" (Google Sheet)
  slack/#engineering                       acme-corp
  slack/#general                           acme-corp
  slack/@alice                             acme-corp
```

When no context is set, `pigeon list` shows all accounts grouped by
platform as it does today.

`pigeon list --since=DURATION` filters to sources with recent activity.

### Grep

`pigeon grep` searches content within the active context:

```
pigeon grep "deploy" --since=7d            # all sources in context
pigeon grep "deploy" --source=slack        # only Slack sources
pigeon grep "quarterly" --source=drive     # only Drive docs
```

The `--source` flag on grep is optional. When omitted, all sources in
the context are searched.

### Glob

`pigeon glob` returns file paths within the active context:

```
pigeon glob --since=7d                     # all data files in context
pigeon glob --since=7d --source=gmail      # only Gmail JSONL files
```

---

## Resolution Chain

Given a command like:

```
PIGEON_CONTEXT=work pigeon read calendar --since=7d
```

Resolution proceeds as follows:

1. **Context**: `PIGEON_CONTEXT=work` → load the `work` context from
   config → accounts: `{gws: work@company.com, slack: acme-corp}`
2. **Source**: `calendar` → GWS service → look for GWS accounts in
   context → `work@company.com`
3. **Selector**: none provided → default: `primary` calendar
4. **Filters**: `--since=7d` → events from the last 7 days
5. **Read**: open `gws/{account-slug}/gcalendar/primary/` → parse
   JSONL date files in range → deduplicate by ID → exclude cancelled →
   sort by start time → format output

If step 2 finds zero GWS accounts in the context:

```
error: no GWS account in context "work" — cannot read calendar
```

If step 2 finds multiple GWS accounts (context has two):

```
error: 2 GWS accounts in context "work" (work@company.com, team@company.com)
       specify one with -a
```

---

## Per-Source Read Algorithms

These algorithms define how raw on-disk data becomes structured output.
Readers never assume files are sorted or deduplicated — correctness does
not depend on maintenance having run.

### Gmail

1. Collect all email lines from JSONL date files in the requested range.
2. Deduplicate by `id` (keep last occurrence).
3. Apply deletes: exclude emails with a matching `email-delete` line.
4. Sort by timestamp.
5. Apply `--last=N` if specified (take last N after sort).
6. Optionally group by `threadId` for threaded display.

### Calendar

1. Collect all event lines from JSONL date files in the requested range.
2. Deduplicate by `id` (keep last occurrence — latest state wins).
3. Exclude cancelled events (`status: "cancelled"`).
4. Sort by start time.

### Drive (Google Docs)

1. Fuzzy-match the selector against drive file directory slugs.
2. Read `{TabName}.md` files and present as markdown.
3. Parse `comments.jsonl`, deduplicate by `id` (keep last occurrence).
4. Display comments grouped by anchor text; replies are in the
   comment's `replies` array.
5. Resolved comments can be shown or hidden based on user preference.

### Drive (Google Sheets)

1. Fuzzy-match the selector against drive file directory slugs.
2. Read `{SheetName}.csv` files and present as tables.
3. Comments: same as Google Docs.

### Slack

1. Resolve channel/DM name in the context's Slack workspace(s).
2. Read from the messaging storage protocol (see `protocol.md`):
   parse date files, deduplicate, reconcile edits/deletes, attach
   reactions, sort by timestamp.
3. Apply `--last=N` if specified.

### WhatsApp

1. Fuzzy-match contact name against conversation directories.
2. Read from the messaging storage protocol: parse date files,
   deduplicate, sort by timestamp.
3. Apply `--last=N` if specified.

---

## Person Filter (Future)

A person exists as different identities across sources:

| Source | Identity format |
|--------|----------------|
| Gmail, Calendar, Drive | Email address |
| Slack | User ID + display name (profile may include email) |
| WhatsApp | Phone number + display name |

The `--from` and `--with` filters will narrow results to items involving
a specific person:

```
pigeon read gmail --from=alice --since=7d         # emails from Alice
pigeon read calendar --with=alice --since=7d      # events with Alice
pigeon read slack #general --from=alice --since=2h # Alice's messages
pigeon grep --person=alice --since=7d             # everything involving Alice
```

Person resolution maps a name to per-source identifiers. The identity
model — how pigeon discovers, stores, and resolves cross-source
identities — is defined separately. The read protocol treats person
filters as opaque: given a resolved set of per-source identifiers, the
read algorithm filters by those identifiers.

**Person as selector vs. person as filter:**

- **Selector**: the resource IS the person. `pigeon read whatsapp Alice`
  opens Alice's conversation. `pigeon read slack @alice` opens the DM.
- **Filter**: narrow within a resource. `pigeon read gmail --from=alice`
  filters the inbox. `pigeon read slack #general --from=alice` filters a
  channel.

These are distinct roles. The selector picks the container; the filter
picks items within it.

---

## Error Behavior

The read layer must never silently return empty results when the
requested data exists but the selection is wrong. Specific error cases:

| Condition | Behavior |
|-----------|----------|
| Source type not recognized | Error: `unknown source "gcalendar" — valid sources: gmail, calendar, drive, slack, whatsapp` |
| No account for source in context | Error: list available accounts and suggest `-a` |
| Multiple accounts, no disambiguator | Error: list accounts, suggest `-a` or context |
| Selector required but missing | Error: `pigeon read drive requires a document name` |
| Selector matches nothing | Error: `no channel matching "eng" in acme-corp` with candidates if close matches exist |
| Filters match no data | Empty result (this is a valid outcome — the data genuinely doesn't exist for that range) |

The distinction: **selection errors** (wrong source, missing selector,
ambiguous account) are loud failures. **Empty results from valid
selections** (no emails today, no events this week) are quiet.
