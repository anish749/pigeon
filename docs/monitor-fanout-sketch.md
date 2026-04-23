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
    EventReaction EventKind = "reaction"
    EventSystem   EventKind = "system" // daemon-lifecycle; deferred
)

// Event is a single notification published to the broadcast bus.
// Content is pre-formatted once at publish time so N subscribers share
// the same formatting work.
type Event struct {
    Kind         EventKind
    Ts           time.Time
    Acct         account.Account
    Conversation string
    Content      string         // formatted text, same shape as session delivery
    Meta         map[string]any // platform, account, conversation, kind, msg_id, ...
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
// Returns the event channel and a cancel func that unsubscribes and
// closes the channel. The cancel func is idempotent.
func (b *Broadcast) Subscribe(filter Filter, bufSize int) (<-chan Event, func())

// Publish fans an event out to every matching subscriber. Non-blocking:
// if a subscriber's buffer is full, the event is dropped for that
// subscriber and a warning is logged.
func (b *Broadcast) Publish(e Event)
```

Conversation-level filtering and event-kind filtering are deferred.
Client-side `jq`/`grep` covers both for v1.

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

Reaction path is already carrying the payload via `ReactionInfo`, so
`RouteReaction`'s signature stays the same.

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
Client connects:  GET /api/tail?since=2026-04-01T00:00:00Z&workspace=eng

Handler:
  1. Subscribe to broadcast (buffer live events in memory as they arrive)
  2. Resolve filter: workspace → []account, or (platform, account)
  3. Historical replay from disk via read.Glob + store, up to replayStart
  4. Drain buffered live events where ts >= replayStart
  5. Stream live until client disconnects, then unsubscribe
```

- No server state between connections.
- `since=` can be arbitrarily old; streamed as long as the files exist.
- Steps 1→4 close the replay/live gap (standard log-tail dedup pattern).
- Monitor dies and restarts → client resends `since=`. Server remembers nothing.

## 4. Wiring

`Hub.New` constructs the broadcast alongside sessions/channels. The new
SSE handler (`TailHandler`) lives next to the existing `SSEHandler` in
`internal/hub/` and uses `h.broadcast.Subscribe(...)` plus `h.store` /
`read` for historical replay.

## 5. Open questions / deferred

- **Workspace expansion** — done at the HTTP handler using
  `internal/workspace`, resolving `workspace=foo` to `[]account.Account`
  and populating `Filter.Accounts`.
- **Conversation-level filtering** — deferred; client-side filtering
  handles v1.
- **Dropped event observability** — `Publish` logs on drop; consider
  emitting a synthetic `{"_dropped": N}` frame later.
- **System events** (listener crashed, account reconnected) — same bus,
  `EventSystem` kind, add later.
- **Reaction enrichment** — monitor sees fallback-formatted reactions
  only; revisit if consumers need parent-message context inline.
