# Pigeon Storage Protocol V1

Pigeon stores messaging events as plain text files, one line per event,
greppable with standard tools. All files are UTF-8 encoded.

## Directory Layout

```
~/.local/share/pigeon/                          # data root
├── slack/                                      # platform
│   └── {workspace-slug}/                       # account (slug, e.g. acme-corp)
│       ├── .maintenance.json                   # maintenance state (see Maintenance Protocol)
│       ├── .sync-cursors.yaml                  # sync state (per-channel cursors)
│       ├── #{channel}/                         # public channel conversation
│       │   ├── YYYY-MM-DD.txt                  # date file (rolling, one per day)
│       │   ├── attachments/                    # attachment storage
│       │   │   └── {ATTACHMENT_ID}.{ext}       # downloaded file
│       │   └── threads/
│       │       └── {THREAD_TS}.txt             # thread file (one per thread)
│       ├── @{user}/                            # DM conversation
│       │   ├── YYYY-MM-DD.txt
│       │   ├── attachments/
│       │   └── threads/
│       │       └── {THREAD_TS}.txt
│       └── @mpdm-{participants}/               # group DM conversation
│           ├── YYYY-MM-DD.txt
│           ├── attachments/
│           └── threads/
│               └── {THREAD_TS}.txt
└── whatsapp/                                   # platform
    └── {phone-slug}/                           # account (slug, e.g. 15551234567)
        ├── .maintenance.json                   # maintenance state
        ├── {contact_or_phone}/                 # 1:1 conversation
        │   ├── YYYY-MM-DD.txt
        │   └── attachments/
        └── {group-name-slug}/                   # group conversation (slugified)
            ├── YYYY-MM-DD.txt
            └── attachments/
```

- **Platform**: `slack` or `whatsapp`.
- **Account**: slugified account name. Slack: workspace name slug
  (e.g. `acme-corp`). WhatsApp: phone digits slug
  (e.g. `15551234567`, no `+` prefix because slug strips it).
- **Conversation**: Slack uses channels (`#name`), DMs (`@user`), and group
  DMs (`@mpdm-...`). WhatsApp DMs use E.164 phone numbers as-is from the
  JID (e.g. `+14155551234`). WhatsApp groups use the slugified group name
  (e.g. `book-club-nyc`).
- **Date files** roll daily. One `YYYY-MM-DD.txt` is created per day with activity.
- **Thread files** are named by the thread parent's platform timestamp.
  For Slack, this is also the parent's message ID.
- **Attachment files** are stored in a per-conversation `attachments/`
  directory, named by their platform attachment ID with original extension.
- WhatsApp does not currently have thread files.

## File Types and Operations

**Two file types** exist:

1. **Date files**: one per conversation per day, rolling. Messages and
   reactions for `2026-03-16` go in `2026-03-16.txt`. The next day starts
   a new file. Old files are never appended to, except when a reaction
   targets a message from that day.

2. **Thread files**: one per thread, named by the parent message's platform
   timestamp. Contains the parent, replies, channel context
   (`threadContextPerDirection = 7` messages before and 7 after the parent),
   and reactions.

**Three operations** happen on these files:

1. **Append** (hot path): real-time listeners, history sync, and the send
   API all append lines. Writes are always append-only. No reads, no dedup
   on write. Multiple concurrent writers are serialized by a per-file
   mutex. The mutex protects only the write call itself (no file reads
   inside the lock). Files are opened with `O_APPEND` so the kernel
   atomically seeks to EOF before each write, preventing data overwrites.
   The mutex prevents interleaving of concurrent writes to the same file.

2. **Maintenance** (background): deduplicates, reconciles edits/deletes
   and react/unreact pairs, sorts, and rewrites the file. This is the only
   operation that rewrites files. Runs after sync completes, or periodically
   as a background task. Only processes files that were appended to since
   the last maintenance pass. Older date files that have not been modified
   are never touched. See the Maintenance Protocol section for full details.

3. **Read** (on demand): parses lines, deduplicates in-memory, sorts
   in-memory by timestamp, applies edits/deletes, aggregates reactions by
   message ID, and produces structured output. Readers never assume the
   file is sorted or deduplicated. Correctness does not depend on
   maintenance having run.

**On-disk ordering guarantees:**

- After maintenance, message lines are in chronological order by timestamp,
  reaction lines follow their target message, and edit/delete pairs have
  been reconciled.
- Between maintenance passes, files may contain duplicates and lines may
  be out of order. This is expected and valid.
- Maintenance is never on the hot path. It does not block appends or reads.

## Identity Standards

All identifiers are stored as-is from the platform. Pigeon does not add
or remove `+` symbols or otherwise transform IDs.

Directory names that require slugification use `gosimple/slug` v1.x rules
(e.g. `Acme Corp` → `acme-corp`, `+15551234567` →
`15551234567`, `Book Club NYC` → `book-club-nyc`).

| Context | Format | Example |
|---------|--------|---------|
| Account directory | Slug of account name | `acme-corp`, `15551234567` |
| WhatsApp conversation dir (DM) | E.164 from JID `"+" + jid.User` | `+14155551234` |
| WhatsApp conversation dir (group) | Slugified group name | `book-club-nyc` |
| `from` Slack | Raw user ID | `U04ABCDEF` |
| `from` WhatsApp JID | Raw JID as-is | `14155551234@s.whatsapp.net` |
| `from` WhatsApp LID | Raw LID as-is | `abc123@lid` |
| Sender name fallback (WhatsApp) | E.164 `"+" + jid.User` | `+14155551234` |

The `from` field always contains the raw platform identifier. The
conversation directory and sender name display may use E.164 format with
a `+` prefix for human readability, but this is a display convention
derived from the JID, not a transformation of the stored ID.

## Escaping

**One message = one line. Always.** To enforce this:

| Character in original text | Stored as | On read, decoded back to |
|----------------------------|-----------|--------------------------|
| newline (`\n`)             | `\n` (literal backslash + n) | newline |
| backslash (`\`)            | `\\` (two backslashes)       | backslash |

Escaping is applied to the **message text** field only (everything after
`Sender Name:`). Sender names, emoji values, and bracket-tag values must
not contain newlines or unescaped backslashes.

This ensures:

- `wc -l` counts events accurately.
- `grep` matches are always complete events.
- `sort` on the timestamp prefix gives chronological order.
- `bufio.Scanner` reads exactly one event per scan.

## Parsing

### Line Structure

Every line (except the separator) follows this structure:

```
[TIMESTAMP] [TAG:VALUE]... Sender Name: content
```

For thread replies, a 2-space indent prefix comes before the timestamp.

### Bracket Tags

Bracket tags appear between the timestamp and the sender name. They are
`[key:value]` pairs. **Tag ordering is not guaranteed.** Parsers must not
assume tags appear in a specific order.

The set of recognized tag keys and how they determine line type:

| Tag key | Determines line type | Present on |
|---------|---------------------|------------|
| `id` | message | messages only |
| `react` | reaction | reactions only |
| `unreact` | unreaction | unreactions only |
| `edit` | edit | edits only |
| `delete` | delete | deletes only |
| `from` | (sender identity) | all line types |
| `via` | (message pathway) | any line type, optional |
| `attach` | (attachment ref) | messages only, optional, repeatable |
| `reply` | (quote-reply ref) | messages only, optional |

### Parsing Algorithm

To parse a line:

1. Strip optional 2-space indent prefix (if present, the line is a thread reply).
2. Parse the timestamp: the first `[...]` bracket group, always 28 chars.
3. Consume all subsequent `[...]` bracket groups as tags. A bracket group
   starts with `[` and ends with `]`. Each tag has the form `[key:value]`
   or `[key:value key=value]` (for attach tags with attributes).
4. The first token after all bracket tags that is **not** enclosed in `[...]`
   is the start of the sender name.
5. The sender name extends to the first `:` character. The `:` is the
   delimiter between sender name and content.
6. Everything after `:` is the content (message text, emoji, or empty).

**Sender name colon stripping**: if a platform display name contains `:`
characters (e.g. "Dr. Smith: Cardiologist"), the `:` characters are
stripped from the sender name at write time. This is an accepted lossy
transformation. The `from` tag carries the stable identity; the
sender name is best-effort for display.

### Line Type Classification

The line type is determined by the **first distinguishing tag**:

| Tag present | Line type |
|-------------|-----------|
| `[id:...]` | message |
| `[react:...]` | reaction |
| `[unreact:...]` | unreaction |
| `[edit:...]` | edit |
| `[delete:...]` | delete |
| `--- channel context ---` | separator (literal string, no tags) |

## Line Types

Every line is one of: **message**, **reaction**, **unreaction**, **edit**,
**delete**, or **separator**.

### Message Line

```
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] Sender Name: message text here
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] [via:VALUE] Sender Name: message text here
```

- **Timestamp**: `[YYYY-MM-DD HH:MM:SS +00:00]`, always 28 chars including
  brackets. Always stored in UTC. Go format string: `2006-01-02 15:04:05 -07:00`
  applied to a `time.Time` value converted to UTC before formatting. The
  reader converts to the user's local timezone for display.
- **Message ID** `[id:VALUE]`: platform-specific message identifier.
  Slack uses the message timestamp (e.g. `1711568938.123456`).
  WhatsApp uses the message key ID (e.g. `3EB0A1B2C3D4E5F6`).
- **Sender ID** `[from:VALUE]`: platform user identifier, stored as-is
  from the platform. No `+` symbols are added or removed.
  Slack: user ID (e.g. `U04ABCDEF`).
  WhatsApp: raw JID or LID as provided by the protocol.
  - **JID** (Jabber ID): phone-number-based, e.g. `14155551234@s.whatsapp.net`.
      The JID `User` field contains digits without a `+` prefix. This is
      stored as-is.
  - **LID** (Linked ID): opaque identifier used when the phone number is
      hidden, e.g. `abc123@lid`.
  The writer resolves LID to JID when possible (via `GetPNForLID`). If
  resolution fails, the LID is stored as-is. JID is preferred because
  it is stable and human-readable.
- **Via** `[via:VALUE]` (optional): present only when pigeon is involved
  in the message pathway. Absent for normal organic messages through the
  user's own connection. Can appear on any line type (messages, reactions,
  edits, deletes).

  | Value | Meaning |
  |-------|---------|
  | `to-pigeon` | A third party sent this to pigeon's bot |
  | `pigeon-as-user` | Pigeon sent this using the user's identity |
  | `pigeon-as-bot` | Pigeon sent this using the bot's identity |

- **Sender Name**: best-effort display name at write time, followed by `:`.
  For WhatsApp, resolved in priority order: FullName (address book) >
  PushName (user's self-chosen name) > BusinessName > phone number.
  For Slack, resolved from the workspace user directory.
  The name may change over time; the `from` JID/user ID is the
  stable identity. Colons in display names are stripped at write time
  (see Parsing Algorithm).
- **Message Text**: everything after `:`, with escaping applied (see Escaping).

Thread replies are prefixed with two spaces:

```
  [TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] Sender Name: reply text
```

### Message with Attachment

```
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] [attach:ATTACH_ID type=image/jpeg] Sender Name: caption text
```

- **`[attach:ATTACH_ID type=MIME_TYPE]`**: references a file stored in the
  conversation's `attachments/` directory.
- **ATTACH_ID**: platform attachment identifier. The file is stored at
  `attachments/{ATTACH_ID}.{ext}` where ext is derived from the MIME type.
- **type**: MIME type (e.g. `image/jpeg`, `video/mp4`, `application/pdf`).
- **Message Text**: the caption or description. May be empty (`:` with
  nothing after it) for media-only messages.

A message may have multiple `[attach:...]` tags:

```
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] [attach:F1 type=image/jpeg] [attach:F2 type=image/png] Alice: two photos
```

### Quote-Reply (WhatsApp)

```
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] [reply:QUOTED_MSG_ID] Sender Name: reply text
```

- **`[reply:QUOTED_MSG_ID]`**: references the message being replied to by
  its ID. This is a single-level, flat reference. It does not create a
  thread or a chain. If someone quote-replies to a quote-reply, each reply
  independently points back to its quoted message.
- The quoted message ID matches an `[id:...]` value in the same
  conversation (possibly in an older date file). If the quoted message
  predates pigeon's sync, the ID may not resolve. Readers handle this
  gracefully by showing the reply without quote context.
- The `QuotedMessage` content from the WhatsApp protobuf is not stored.
  The original message is already in storage, reachable by ID.
- This is distinct from Slack threads. Slack uses separate thread files
  with parent/reply structure. WhatsApp quote-replies stay inline in the
  date file as regular messages with a `[reply:...]` tag.

### Empty Text (Media-Only Message)

```
[TIMESTAMP] [id:MSG_ID] [from:SENDER_ID] [attach:F07T3 type=image/jpeg] Alice:
```

When a message has an attachment but no caption, the text after `:` is empty.
This is a regular message line, not a special line type.

### Reaction Line

```
[TIMESTAMP] [react:MSG_ID] [from:SENDER_ID] Sender Name: EMOJI
```

- **`[react:MSG_ID]`**: references the message being reacted to by its ID.
- **Emoji**: the emoji name or Unicode character. Slack uses names
  (e.g. `thumbsup`, `tada`). WhatsApp uses Unicode emoji (e.g. `👍`, `🎉`).
  Both formats are valid. The protocol does not standardize to one form.

### Unreaction Line

```
[TIMESTAMP] [unreact:MSG_ID] [from:SENDER_ID] Sender Name: EMOJI
```

Same as reaction but indicates removal. The reader reconciles react/unreact
events to compute the final reaction state per message.

### Edit Line

```
[TIMESTAMP] [edit:MSG_ID] [from:SENDER_ID] Sender Name: updated message text
[TIMESTAMP] [edit:MSG_ID] [from:SENDER_ID] [attach:ATTACH_ID type=image/jpeg] Sender Name: updated caption
```

- **`[edit:MSG_ID]`**: references the message being edited by its ID.
- **Message Text**: the new full text of the message, with escaping applied.
- **Attachments**: an edit can add, change, or remove attachments. The
  `[attach:...]` tags on the edit line represent the complete set of
  attachments after the edit. If the edit has no `[attach:...]` tags,
  the message has no attachments after the edit.
- Appended like any other event. The reader replaces the original message
  text and attachments with the most recent edit (by timestamp).
  Maintenance replaces the original message line with the edited content
  and removes the edit line.

### Delete Line

```
[TIMESTAMP] [delete:MSG_ID] [from:SENDER_ID] Sender Name:
```

- **`[delete:MSG_ID]`**: references the message being deleted by its ID.
- Text after `:` is empty.
- Appended like any other event. The reader excludes deleted messages from
  output. Maintenance removes both the original message and the delete line.

### Separator Line

```
--- channel context ---
```

Only appears in thread files. Separates thread replies from surrounding
channel context.

## File Structures

### Date File

Path: `{data_dir}/{platform}/{account}/{conversation}/YYYY-MM-DD.txt`

A chronological log of messages and reactions for one conversation on one day.
A new file is created for each day (rolling). Reactions targeting a past day's
message are appended to that day's file, not today's.

```
[2026-03-16 09:15:02 +00:00] [id:1711568938.123456] [from:U04ABCD] Alice: hello world
[2026-03-16 09:15:30 +00:00] [id:1711568940.789012] [from:U04EFGH] [attach:F07T3 type=image/jpeg] Bob: check this out
[2026-03-16 09:16:00 +00:00] [id:1711568942.111111] [from:U04USER] [via:pigeon-as-user] User: looks great Bob!
[2026-03-16 09:16:30 +00:00] [react:1711568938.123456] [from:U04EFGH] Bob: 👍
[2026-03-16 09:17:00 +00:00] [id:1711568944.222222] [from:U04ALICE] [via:to-pigeon] Alice: hey pigeon, summarize this channel
[2026-03-16 09:17:15 +00:00] [id:1711568945.333333] [from:U04BOT] [via:pigeon-as-bot] pigeon: sure, working on it
```

### Thread File

Path: `{data_dir}/{platform}/{account}/{conversation}/threads/{THREAD_TS}.txt`

Contains the thread parent, replies, surrounding channel context, and reactions.

```
[2026-03-16 09:15:30 +00:00] [id:1711568940.789012] [from:U04EFGH] Bob: starting a thread
  [2026-03-16 09:16:00 +00:00] [id:1711568960.345678] [from:U04ABCD] Alice: replying here
  [2026-03-16 09:17:00 +00:00] [id:1711568980.456789] [from:U04EFGH] Bob: thanks
--- channel context ---
[2026-03-16 09:13:00 +00:00] [id:1711568800.111111] [from:U04XYZW] Charlie: context before
[2026-03-16 09:14:00 +00:00] [id:1711568860.222222] [from:U04ABCD] Alice: more context before
[2026-03-16 09:18:00 +00:00] [id:1711569000.333333] [from:U04XYZW] Charlie: context after
[2026-03-16 09:20:00 +00:00] [react:1711568940.789012] [from:U04ABCD] Alice: 🎉
```

Structure:

1. **Parent message**: first line, unindented.
2. **Replies**: indented with two spaces.
3. **`--- channel context ---`**: separator.
4. **Channel context**: `threadContextMessages = 7` messages before and 7
   after the parent from the channel, unindented. This is a protocol-level
   constant.
5. **Reactions**: appended at the end.

The thread file name (`THREAD_TS`) is the parent message's platform timestamp.
For Slack, this is also the parent's message ID.

**Thread parent duplication is intentional.** The parent message exists in
both the date file and the thread file. This is by design so that each
thread file is self-contained: a reader can open a single thread file and
see the parent, all replies, and surrounding channel context without
needing to cross-reference the date file.

## Write Protocol

### Append

All writes acquire a per-file mutex, open the file with
`O_APPEND | O_CREATE | O_WRONLY`, write a single line, and release the
mutex. No file reads, no dedup checks. The write path is as simple as
possible.

Multiple concurrent writers (real-time listener, history sync, send API)
all append to the same files safely. The per-file mutex serializes writes
to prevent interleaving. `O_APPEND` ensures the kernel seeks to EOF
before each write, preventing data overwrites. Different files use
independent mutexes, so writes to different conversations do not contend.

Deduplication is handled by the maintenance pass and by readers.

### Reaction Placement

When a reaction event arrives, the writer:

1. Extracts the target message's original timestamp from the reaction event
   (both Slack and WhatsApp provide this).
2. Derives the date file: `ts.Format("2006-01-02") + ".txt"`.
3. Appends the reaction line to that file.

Reactions on thread messages are appended to the thread file.

### Edit and Delete Placement

Same rule as reactions. The writer derives the date file from the
**original message's** timestamp, not the edit/delete event's own
timestamp. This ensures the edit/delete line lands in the same file as
the message it targets.

Edits and deletes on thread messages are appended to the thread file.

### Attachment Storage

When a message with an attachment arrives, the writer:

1. Writes the message line with the `[attach:...]` tag. The tag is always
   present if the original message had an attachment, regardless of whether
   the file was successfully downloaded.
2. Downloads the file content from the platform (best-effort).
3. Stores it at `attachments/{ATTACH_ID}.{ext}` in the conversation directory.

The message line is the source of truth for whether a message had an
attachment. The file on disk is best-effort — it may be missing due to
download failures, expired URLs, or network issues. Readers must not
assume the referenced file exists. Attachment files are immutable once
written. They are never updated or deleted.

## Maintenance Protocol

### State File

Path: `{data_dir}/{platform}/{account}/.maintenance.json`

Tracks the last-maintained modification time for each file in the account:

```json
{
  "#engineering/2026-03-16.txt": "2026-03-16T18:30:00Z",
  "#engineering/threads/1711568940.789012.txt": "2026-03-16T18:30:00Z",
  "@alice/2026-04-01.txt": "2026-04-01T22:00:00Z"
}
```

Keys are file paths relative to the account directory. Values are the file's
`mtime` at the time maintenance last processed it.

One state file per account, stored alongside `.sync-cursors.yaml`.

### Concurrency

Maintenance runs in the same daemon process as the writers. It acquires
the same per-file mutex before reading or rewriting a file. Writers block
while maintenance holds the lock. No external lock file is needed.

### Run Procedure

1. Load `.maintenance.json` (empty map if missing).
2. Walk all `.txt` files in the account directory.
3. `stat` each file to get current `mtime`.
4. Skip files where `mtime` equals the stored timestamp (unchanged).
5. For each changed file (acquire per-file mutex before proceeding):
   a. Parse all lines, classify by type.
   b. Deduplicate message lines by message ID (keep first occurrence).
   c. Deduplicate reaction lines by (message ID, emoji, sender ID) tuple.
   d. Reconcile react/unreact pairs. For each (message ID, emoji, sender ID)
      tuple, replay events in timestamp order. If the final state is
      "unreacted", remove both the react and unreact lines. If the final
      state is "reacted" (e.g. react, unreact, react), keep only the
      surviving react line and remove the intermediate pairs.
   e. Apply edits. For each edit line, replace the original message's text
      with the edited text. If multiple edits exist for the same message,
      use the most recent one (by timestamp). Remove all edit lines after
      applying.
   f. Apply deletes. For each delete line, remove the original message line
      and the delete line. Also remove any reactions, edits, or unreactions
      that reference the deleted message.
   g. Sort message lines by timestamp (stable sort).
   h. Relocate reaction lines after their target message.
   i. Reactions referencing an unknown message ID stay at the end.
   j. Rewrite the file only if content changed. Release the per-file mutex.
6. Update `.maintenance.json` with current `mtime` for each processed file.

### When Maintenance Runs

- After sync completes (per-account).
- Optionally as a periodic background task.
- Never on the hot path. Does not block appends or reads.

## Read Protocol

The reader never assumes the file is sorted or deduplicated. It always
performs these steps in-memory:

1. **Parse**: read all lines, classify each by its bracket tags (see
   Parsing section).

2. **Deduplicate**: remove duplicate message lines (by message ID, keep
   first occurrence) and duplicate reaction lines by (message ID, emoji,
   sender ID) tuple.

3. **Sort**: order message lines by timestamp (stable sort).

4. **Apply edits**: for each edit line, replace the original message's text
   with the most recent edit (by timestamp).

5. **Apply deletes**: exclude messages that have a corresponding delete line.

6. **Aggregate reactions**: build a map of `msgID -> []Reaction`, then
   reconcile react/unreact events to produce the final state per message.

7. **Output**: produce structured result for display or search.

### Display

Messages are shown with their aggregated reactions beneath:

```
[09:15:02] Alice: hello world
    👍 Bob · 🎉 Charlie
[09:15:30] Bob: check this out [📎 image/jpeg]
```

The `[id:...]`, `from`, `[via:...]`, and `[attach:...]` tags are
stripped for display. They are internal metadata. Attachments may be shown
as a summary indicator.

## Data Model

```go
package model

// Via represents the message pathway through pigeon.
type Via string

const (
    ViaOrganic      Via = ""               // normal message, user's own connection
    ViaToPigeon     Via = "to-pigeon"      // third party sent this to pigeon's bot
    ViaPigeonAsUser Via = "pigeon-as-user" // pigeon sent using the user's identity
    ViaPigeonAsBot  Via = "pigeon-as-bot"  // pigeon sent using the bot's identity
)

// LineType classifies a parsed line.
type LineType int

const (
    LineMessage   LineType = iota // [id:...]
    LineReaction                  // [react:...]
    LineUnreaction                // [unreact:...]
    LineEdit                     // [edit:...]
    LineDelete                   // [delete:...]
    LineSeparator                // --- channel context ---
)

type MsgLine struct {
    ID          string       // platform message ID
    Ts          time.Time    // message timestamp
    Sender      string       // display name (best-effort at write time)
    SenderID    string       // platform user ID (stable identity)
    Via         Via          // message pathway
    ReplyTo     string       // quoted message ID (WhatsApp quote-reply), empty if not a reply
    Text        string       // message body (may contain newlines)
    Reply       bool         // thread reply (2-space indent)
    Attachments []Attachment // zero or more attachments
}

type Attachment struct {
    ID   string // platform attachment ID (filename in attachments/)
    Type string // MIME type (e.g. "image/jpeg")
}

type ReactLine struct {
    Ts       time.Time // when the reaction happened
    MsgID    string    // target message ID
    Sender   string    // who reacted (display name)
    SenderID string    // who reacted (platform ID)
    Via      Via       // message pathway
    Emoji    string    // emoji name or Unicode character
    Remove   bool      // true = unreact
}

type EditLine struct {
    Ts          time.Time    // when the edit happened
    MsgID       string       // target message ID
    Sender      string       // who edited (display name)
    SenderID    string       // who edited (platform ID)
    Via         Via          // message pathway
    Text        string       // new message text
    Attachments []Attachment // complete attachment set after edit
}

type DeleteLine struct {
    Ts       time.Time // when the delete happened
    MsgID    string    // target message ID
    Sender   string    // who deleted (display name)
    SenderID string    // who deleted (platform ID)
    Via      Via       // message pathway
}

type DateFile struct {
    Messages  []MsgLine
    Reactions []ReactLine
    Edits     []EditLine
    Deletes   []DeleteLine
}

type ThreadFile struct {
    Parent    MsgLine
    Replies   []MsgLine
    Context   []MsgLine   // 7 before + 7 after parent
    Reactions []ReactLine
    Edits     []EditLine
    Deletes   []DeleteLine
}
```

## Greppability

Each event is one JSON object per line (JSONL). Standard text tools work
directly on the files. `pigeon rg` (or `pigeon grep` if ripgrep is not
installed) wraps these tools with platform/account scoping and date
filtering.

### rg / grep

Plain-text search works because field values appear as literal strings
in the JSON:

```bash
rg "Alice" file.txt                   # messages involving Alice
rg "deploy" file.txt                  # full-text search
rg "thumbsup" file.txt               # reactions with this emoji
rg "image/jpeg" file.txt             # messages with JPEG attachments
rg "pigeon-as" file.txt              # messages sent by pigeon
rg "to-pigeon" file.txt              # messages sent to pigeon's bot
rg -c "" file.txt                    # count total events
wc -l file.txt                       # count total events (same)
rg -C 3 "deploy" file.txt            # match with 3 lines of context
```

### jq

For structured queries, pipe through `jq`. Each line is valid JSON:

```bash
# Select by event type
jq -c 'select(.type == "msg")' file.txt
jq -c 'select(.type == "react")' file.txt
jq -c 'select(.type == "edit")' file.txt

# Select by sender
jq -c 'select(.sender == "Alice")' file.txt

# Select by via (message pathway)
jq -c 'select(.via == "to-pigeon")' file.txt
jq -c 'select(.via != null and (.via | startswith("pigeon-as")))' file.txt

# Full-text search within message text
jq -c 'select(.text != null and (.text | contains("deploy")))' file.txt

# Messages with attachments
jq -c 'select(.attach != null)' file.txt

# Compound queries
jq -c 'select(.sender == "Bob" and .attach != null)' file.txt
jq -c 'select(.type == "msg" and .ts > "2026-03-16T09:00:00Z")' file.txt

# Extract specific fields
jq -r 'select(.type == "msg") | .id' file.txt
jq -r 'select(.type == "edit") | .msg' file.txt

# Format as readable output
jq -r 'select(.type == "msg") | "[" + .ts[11:19] + "] " + .sender + ": " + (.text // "")' file.txt

# Sort by timestamp
jq -s 'sort_by(.ts)[]' file.txt

# Count messages only
jq -c 'select(.type == "msg")' file.txt | wc -l

# Group reactions by emoji
jq -s '[.[] | select(.type == "react")] | group_by(.emoji) | map({emoji: .[0].emoji, count: length})' file.txt
```

### Combining rg and jq

Use `rg` for fast filtering, then `jq` for structured extraction:

```bash
# Find messages mentioning "deploy", format as readable lines
rg "deploy" file.txt | jq -r 'select(.type == "msg") | "[" + .ts[11:19] + "] " + .sender + ": " + .text'

# Search across all conversations, extract sender and text
rg "bug" ~/.local/share/pigeon/ | jq -r 'select(.type == "msg") | .sender + ": " + .text'
```

### Scoping by directory

The directory layout provides natural scoping:

```bash
# All platforms, all accounts
rg "deploy" ~/.local/share/pigeon/

# Slack only
rg "deploy" ~/.local/share/pigeon/slack/

# Specific account
rg "deploy" ~/.local/share/pigeon/slack/acme-corp/

# Specific conversation
rg "deploy" ~/.local/share/pigeon/slack/acme-corp/#general/
```

## Notes

### Outgoing Messages

Outgoing messages (sent via `pigeon send` or the MCP send tool) use the
same line format as incoming messages. The `from` field contains the
account's own platform user ID (WhatsApp: own JID, Slack: bot user ID or
authenticated user ID). The sender name is resolved the same way as for
any other user. There is no special line type for outgoing messages.
Whether a message is "yours" is determined by comparing `from` to
the account's own ID.

### Bot Conversations (Slack)

In Slack, pigeon operates with two tokens: a **user token** (the human
user's own Slack identity) and a **bot token** (the pigeon app's bot
identity). The bot has its own DM channels. A third party can message
the pigeon bot directly, and pigeon can reply on behalf of the user.

Bot DM messages and the user's own DM messages with the same contact
are stored in the **same conversation directory**. For example, if Alice
DMs the pigeon bot and the user also has a DM with Alice, both
conversations are stored in `@Alice/`. This creates a unified timeline
where the user sees all communication with a contact in one place.

The `[via:...]` tag distinguishes all message pathways. Example
timeline in `@Alice/2026-03-16.txt`:

```
[ts] [id:...] [from:U04ALICE] Alice: hey, are you around?
[ts] [id:...] [from:U04USER] User: hey!
[ts] [id:...] [from:U04ALICE] [via:to-pigeon] Alice: hey pigeon, schedule a call with User?
[ts] [id:...] [from:U04BOT] [via:pigeon-as-bot] pigeon: sure, checking his calendar
[ts] [id:...] [from:U04USER] [via:pigeon-as-user] User: Alice, 3pm works for me
```

| Scenario | `from` | `[via:...]` |
|----------|-------------|-------------|
| User sends directly in Slack | User's ID | (absent) |
| Third party messages the user | Third party's ID | (absent) |
| Third party messages the bot | Third party's ID | `to-pigeon` |
| Pigeon sends as user | User's ID | `pigeon-as-user` |
| Pigeon sends as bot | Bot's ID | `pigeon-as-bot` |

Every case is distinguishable from the line alone, without needing to
know external IDs.

### Thread Interleaving on Read

The read protocol describes parsing a single file, but `pigeon read`
combines date files with thread files for display. This is a display-time
enrichment, not part of the stored format:

1. Read the date file for the requested time range.
2. For each message that is a thread parent (matched by message ID to a
   thread file), annotate it with `[thread:ts]` and splice the thread
   replies (indented) after the parent line.
3. Threads with recent activity whose parent falls outside the time range
   are appended at the end with a blank line separator.

The `[thread:ts]` annotation is never written to disk. It exists only in
the read output.

### System Messages

System messages (channel joins/leaves, topic changes, etc.) are not part
of Protocol V1. They are not stored. This may be revisited in a future
protocol version.

## Storage Format Change: Bracket-Tag → JSONL

The original V1 protocol used a custom bracket-tag line format:

```
[2026-03-16 09:15:02 +00:00] [id:MSG_ID] [from:SENDER_ID] Sender Name: message text
```

This has been replaced with JSONL (one JSON object per line):

```json
{"type":"msg","ts":"2026-03-16T09:15:02Z","id":"MSG_ID","from":"SENDER_ID","sender":"Sender Name","text":"message text"}
```

**Why:** The bracket-tag format required a hand-rolled parser with edge cases
that were difficult to eliminate: brackets in sender names broke the tag
parser, colons in sender names were lossy (stripped at write time), newlines
and backslashes in non-text fields could corrupt the file, and `]` in tag
values truncated them. JSONL delegates all serialization to `encoding/json`,
which handles escaping, quoting, and special characters correctly. The
one-line-per-event property is preserved, as are `grep`, `wc -l`, and `sort`
compatibility.

## Known Limitations

### Do Not

- **Do not rely on `O_APPEND` alone for write safety.** `O_APPEND`
  guarantees atomic seek-to-EOF, but does not guarantee that the write
  itself is atomic for large payloads. Messages can exceed filesystem
  atomic write limits (e.g. a Slack message can be 40,000+ characters).
  Always use a per-file mutex to serialize writes.
- **Do not read the file during the write path.** Deduplication and
  sorting are handled by the maintenance pass and by readers. The write
  path must only append.
- **Do not modify existing lines during append.** Edits, deletes, and
  reactions are appended as new event lines. Only the maintenance pass
  rewrites files.
- **Do not assume files are sorted or deduplicated.** Readers must
  always handle unsorted, duplicated input. Correctness must not depend
  on maintenance having run.
- **Do not use the sender name as an identifier.** Sender names are
  best-effort display names that can change. Use the `from` field for
  identity.
- **Do not add or remove `+` from phone numbers.** Store identifiers
  as-is from the platform. Slug transformations are only for directory
  names.

### Channel and Group Renames Split Conversations

Conversation directories are named by the channel or group name (slugified
for WhatsApp groups). If a channel or group is renamed, the new name
produces a new directory. Messages before the rename stay in the old
directory; messages after go to the new one. The protocol does not handle
merging these. This applies to both Slack channels (e.g. `#engineering`
renamed to `#platform-eng`) and WhatsApp groups. It is a known limitation
of using names as directory keys rather than stable platform IDs.
