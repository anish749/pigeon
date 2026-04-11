# Features

## Attachments

Support file attachments (photos, documents, etc.) in Slack messages and deliver them through to the session so Claude can understand them.

## Reactions

`pigeon react` command is implemented for both Slack and WhatsApp. Slack incoming reaction events are handled (`reaction_added` / `reaction_removed`).

Remaining: handle incoming WhatsApp reaction events. WhatsApp sends `ReactionMessage` in the event handler (`waE2E.Message.ReactionMessage`) with the target message key and emoji text. The listener should extract these and store as `ReactLine` / unreact lines, matching the pattern used in the Slack listener's `handleReaction`.

## WhatsApp edit and delete events

Handle incoming WhatsApp message edits and deletes. WhatsApp supports:
- **Edits**: `waE2E.Message.ProtocolMessage` with type `MESSAGE_EDIT` contains the edited text and the target message key.
- **Deletes**: `waE2E.Message.ProtocolMessage` with type `REVOKE` ("delete for everyone") contains the target message key.

The Slack listener already handles both (`message_changed` and `message_deleted` subtypes). The WhatsApp listener should follow the same pattern: extract the event, construct an `EditLine` or `DeleteLine`, and append to the correct date file.

## Revamp setup / onboarding

The three setup commands (`setup-whatsapp`, `setup-slack`, `setup-gws`) have
diverged in shape and UX, and the root help text is out of date now that GWS
is a first-class platform.

Observable issues:

- ~~**`pigeon` root help omits GWS.**~~ Fixed in #163.
- **Prompt libraries are inconsistent.** `setup-slack` uses `bufio.NewReader`
  with hand-rolled `fmt.Print` prompts, `setup-whatsapp` drives its own
  interactive flow around QR pairing, `setup-gws` uses `promptui`. Three
  setup commands, three prompt styles.
- **Output shapes diverge.** Each command has its own header banner
  ("Slack Workspace Setup\n======"), its own confirmation footer, and its
  own tone. There is no shared scaffolding for "detect state → prompt → save
  → tell the user what to do next."
- **Auth models are very different but that difference isn't surfaced.**
  `setup-slack` runs an OAuth server in-process. `setup-whatsapp` pairs a
  device via QR. `setup-gws` is a thin config writer because `gws` owns auth
  externally. The help text doesn't prepare users for any of this, and the
  commands themselves don't explain where auth lives relative to pigeon.

**Files affected:** `internal/cli/root.go`, `internal/cli/setup.go`,
`internal/commands/setup_slack.go`, `internal/commands/setup_whatsapp.go`,
`internal/commands/setup_gws.go`.

## GWS: `pigeon read` semantics

`pigeon read` is conversation-centric (`--platform --account --contact
--date`). GWS data has no "conversation" analogue:

- Gmail: could read by date, by thread, or by sender.
- Calendar: read by date (events on a day).
- Drive: read a specific file (doc, sheet, comments).

Needs a design pass on the command shape — separate subcommands
(`read-mail`, `read-cal`, `read-drive`) vs a polymorphic `read --platform gws
--service <s> --date X` vs something else entirely.

## GWS: hub delivery to Claude Code sessions

The daemon hub pushes new messaging data to active MCP sessions via SSE
(`internal/hub/hub.go`). GWS pollers write to disk but do not notify the
hub, so Claude Code sessions never see new emails, calendar events, or
Drive file changes in real time.

Needs design decisions on what's delivered (every new email? only
mentions? calendar notifications for today?), at what granularity, and
how to format GWS events for the existing `IncomingMsg` shape — or
whether a new notification type is needed.

## GWS Gmail: attachment download

`EmailLine.Attach` stores attachment metadata (filename, MIME type, Gmail
part ID) but the bytes are never fetched. Claude Code sessions cannot
read or reason about attached files — PDFs, images, spreadsheets, and
other content attached to emails are invisible beyond their names.

Gmail's API exposes attachment bytes via `users.messages.attachments.get`
keyed by message ID and attachment part ID. A feature here would decide
which attachments to fetch (all? only under a size cap? only certain
MIME types?), where to store them (mirror Drive's `attachments/` layout?
a dedicated path?), and how to surface them to the read/hub layers.

## Unified read abstraction across messaging and GWS

The read layer (`store.Store`, `modelv1.Line`, `internal/read`,
`internal/search`) is built around conversational messaging data. GWS
types (`gws/model.EmailLine`, `EventLine`, `CommentLine`, `ReplyLine`)
are separate and don't fit the messaging model. Currently the read layer
is extended ad-hoc per command — glob/grep handle GWS files by adding
extensions and drive-meta discovery, but the match/display path still
assumes `modelv1.MsgLine`.

A unified `Line` interface (or equivalent abstraction) would let
`store.Store`, the formatter, `ParseGrepOutput`, `search.Match`, and hub
delivery work generically across both worlds. The V1 ADR explicitly
deferred this as "the read layer needs to be unified across all
platforms." This is the prerequisite for most of the other items above
being done cleanly rather than as per-platform branches.
