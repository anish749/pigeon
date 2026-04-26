# Features

## Elicitation protocol

### Owner

The owner is the person running pigeon. No explicit configuration is needed — pigeon already knows who the owner is on each connected platform from the credentials used during setup.

### Elicitation

Pigeon runs in the background. When an agent decides it needs input from the owner, it reaches out to the owner directly via one of the connected platforms (Slack, WhatsApp, etc.) and waits.

The owner replies to that message. Pigeon routes the reply back to the right agent and resumes. Thread replies are used for correlation, so multiple agents can have open questions simultaneously without confusion.

The agent waits indefinitely. There is no timeout and no default action. The agent may choose to follow up if the matter is urgent, but otherwise it simply waits.

### Outreach to non-owners

When pigeon wants to contact someone other than the owner — scheduling a meeting, asking availability, gathering information — it does not send that message on its own. It first asks the owner for approval.

The owner can approve from wherever is convenient: the terminal or directly from Slack. Both are equivalent.

The agent can also batch this upfront: it tells the owner "here's what I'm planning to do and who I need to contact" and the owner approves the whole plan at once before anything is sent.

## Attachments

Support file attachments (photos, documents, etc.) in Slack messages and deliver them through to the session so Claude can understand them.

## Reactions

`pigeon react` command is implemented for both Slack and WhatsApp. Slack incoming reaction events are handled (`reaction_added` / `reaction_removed`) and delivered to connected Claude Code sessions (#177).

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

## Conversation review TUI

A terminal UI for browsing stored conversations — useful for auditing bot
DM conversations (what pigeon said to someone, what they replied) without
leaving the terminal or relying on the Slack UI.

The data already exists: `pigeon read` can pull up any conversation from
the local JSONL store for both bot-sent and inbound messages. A TUI would
sit on top of this — list conversations per account, open one, scroll
through the thread, jump between dates.

Quality-of-life rather than critical — the query layer via `pigeon read`
is sufficient for now. Becomes more valuable as the volume of
bot-initiated DM outreach grows.

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

## Workstream TUI: in-app discovery

The workstream TUI (`pigeon workstream tui`, #313) lists and edits
workstreams in the active workspace. It assumes the store is already
populated — there is no way to trigger discovery from inside the UI. An
empty workspace shows "no workstreams; press n to create one manually,"
which doesn't match how the system is meant to be used.

The persistence prerequisite is this PR — once `pigeon workstream
discover` writes its results to the per-workspace store, the TUI needs:

- An empty-state prompt: "no workstreams in this workspace — D to
  discover from your messaging history, n to create manually."
- A `D` key (also available from the populated list) that runs discovery
  in the background and shows a spinner. Discovery is an LLM call that
  can take 30–90 seconds.
- On completion, the list reloads from the store and a status flash
  reports how many were found (or that none were).
- Cancellation via ctrl+c during discovery should leave the store
  untouched.

Open questions for the implementer:
- Discovery uses a default 30-day window today. Should the TUI expose
  since/until knobs, or stay zero-config?
- Re-running discovery overwrites same-named workstreams. Should
  user-edited focus/state survive a re-run, or is "the LLM is the source
  of truth on re-run" the right policy?
- What happens to workstreams the LLM no longer surfaces — leave,
  dormant, or delete?

A starting sketch exists in commits 568893d (TUI wiring + spinner) and
869538b (CLI persistence) on branch `worktree-workstream-tui`. They
were extracted out of #313 / #315 to keep both PRs scoped. They build
but have no tests; treat them as a starting point, not a finished
implementation. Tests should cover at minimum: discovery → persistence
round trip, re-run idempotency, and the cancellation path.
