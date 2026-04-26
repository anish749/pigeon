# `pigeon monitor` — broadcast & route sketch

Design sketch for the fanout layer that feeds a new stateless `/api/tail`
endpoint. Peek-only, no cursor state, no session binding.

## 1. `Broadcast` API

Lives in `internal/hub/`, sibling to the existing per-account channels.
Pure in-memory pub/sub. No history buffer — historical replay is a
separate concern handled by `/api/tail` using `read.Glob` + `store`.

```go
// internal/hub/broadcast.go
package hub

// EventKind distinguishes event types on the broadcast bus.
type EventKind string

const (
    EventMessage  EventKind = "message"
    EventReaction EventKind = "reaction" // reaction added
    EventUnreact  EventKind = "unreact"  // reaction removed
    EventEdit     EventKind = "edit"     // message edited
    EventDelete   EventKind = "delete"   // message deleted
    EventSystem   EventKind = "system"   // tail connection + replay-error frames
)

// Envelope carries the common routing metadata. It embeds account.Account
// so that Platform and Name are promoted as top-level JSON fields.
type Envelope struct {
    Kind            EventKind
    account.Account
    Conversation    string
}

// Notification is the published payload. Concrete types embed Envelope
// plus their own modelv1 line type: NotifMsg/MsgLine, NotifReact/ReactLine,
// NotifEdit/EditLine, NotifDelete/DeleteLine. NotifSystem is separate —
// it carries a Content string for lifecycle frames (connection ready,
// replay error) and is written by the tail handler directly, never
// published through the bus.
type Notification interface { envelope() Envelope }

type NotifMsg struct {
    Envelope
    modelv1.MsgLine
}

type NotifReact struct {
    Envelope
    modelv1.ReactLine
}

type NotifEdit struct {
    Envelope
    modelv1.EditLine
}

type NotifDelete struct {
    Envelope
    modelv1.DeleteLine
}

type NotifSystem struct {
    Kind    EventKind
    Ts      time.Time
    Content string
}

// Filter selects which events a subscriber receives. An empty Accounts
// slice means "any account"; a non-empty slice is an allowlist.
// A single platform+account filter is just a one-element Accounts slice.
// Workspace expansion happens at the HTTP edge, not here.
type Filter struct {
    Accounts []account.Account
}

// Broadcast is the in-memory fanout bus. Publish is non-blocking per
// subscriber — a slow consumer drops events rather than back-pressuring
// the listener hot path.
type Broadcast struct { /* ... */ }

func NewBroadcast() *Broadcast

// Subscribe registers a subscriber with the given filter and buffer size.
// Returns the notification channel and a cancel func that unsubscribes
// and closes the channel. The cancel func is idempotent.
func (b *Broadcast) Subscribe(filter Filter, bufSize int) (<-chan Notification, func())

// Publish fans a notification out to every matching subscriber. Non-blocking:
// if a subscriber's buffer is full, the notification is dropped for that
// subscriber and a warning is logged.
func (b *Broadcast) Publish(n Notification)
```

Conversation-level filtering is deferred. Client-side `jq`/`grep`
covers it for v1.

## 2. `Route` signature change

The listener already holds the parsed `modelv1.MsgLine` right before
calling `onMessage`. Passing it through lets the hub format exactly
once for the broadcast and avoids an extra disk read + race with fsync.

```go
// BEFORE
type MessageNotifyFunc func(acct account.Account, conversation string) RouteResult
func (h *Hub) Route(acct account.Account, conversation string) RouteResult

// AFTER
type MessageNotifyFunc func(acct account.Account, conversation string, msg modelv1.MsgLine) RouteResult
func (h *Hub) Route(acct account.Account, conversation string, msg modelv1.MsgLine) RouteResult
```

Reaction path follows the same pattern: `RouteReaction` takes a
`modelv1.ReactLine` so the listener forwards the exact line it wrote
to disk, no timestamp or field reconstruction.

### Route behavior

On every call:

1. Publish to the broadcast bus (cheap, non-blocking, independent of session state).
2. Signal the per-account session channel — existing behavior, unchanged.

Step 1 fires even when no session is bound for the account. That's the
point: monitor sees all traffic; `RouteNoSession` only reflects session
delivery state.

### Reaction path

Same pattern in `RouteReaction`: publish to broadcast, then signal the
session channel. For broadcast `Content`, use
`modelv1.FormatReactionFallbackNotification` (no disk lookup) so the
listener hot path stays fast. The rich parent-message lookup stays in
`deliverReaction` where it already is for session delivery.

## 3. `/api/tail` is stateless

JSONL files on disk persist across daemon restarts, so history is always
available regardless of when the daemon started. The broadcast bus only
deals with live events from "now" forward.

```
Client connects:  GET /api/tail?q=<encoded request>

Handler:
  1. Decode the request (accounts filter + since)
  2. Subscribe to broadcast
  3. Historical replay from disk via store, if since is set
  4. Stream live until client disconnects, then unsubscribe
```

- No server state between connections.
- `since=` can be arbitrarily old; streamed as long as the files exist.
- Workspace expansion happens at the CLI layer; the handler only sees
  a concrete list of accounts.
- An event that lands in both replay and the live window is emitted
  twice. Consumers that need exactly-once filter on `msg_id`.
- Monitor dies and restarts → client resends the request. Server remembers nothing.

## 4. Wiring

`Hub.New` constructs the broadcast alongside sessions/channels. The new
SSE handler (`TailHandler`) lives next to the existing `SSEHandler` in
`internal/hub/` and uses `h.broadcast.Subscribe(...)` plus `h.store` /
`read` for historical replay.

## 5. Open questions / deferred

- **Conversation-level filtering** — deferred; client-side filtering
  handles v1.
- **Dropped event observability** — `Publish` logs on drop.
- **Listener-lifecycle system events** (listener crashed, account
  reconnected) — same bus, `EventSystem` kind, not emitted today.
  `EventSystem` is currently only used by the tail handler itself.
- **Reaction enrichment** — monitor sees fallback-formatted reactions
  only; revisit if consumers need parent-message context inline.
