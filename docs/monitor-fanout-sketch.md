# `pigeon monitor` — broadcast & route sketch

Design sketch for the fanout layer that feeds a new stateless `/api/tail`
endpoint. Peek-only, no cursor state, no session binding.

## 1. `Broadcast` type

Lives in `internal/hub/`, sibling to the existing per-account channels.
Pure in-memory pub/sub. No history buffer — historical replay is a
separate concern handled by `/api/tail` using `read.Glob` + `store`.

```go
// internal/hub/broadcast.go
package hub

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anish749/pigeon/internal/account"
)

// EventKind distinguishes event types on the broadcast bus.
type EventKind string

const (
	EventMessage  EventKind = "message"
	EventReaction EventKind = "reaction"
	EventSystem   EventKind = "system" // daemon-lifecycle; optional, later
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

// Filter selects which events a subscriber receives. Zero-value fields
// mean "any". Accounts, when non-empty, is an allowlist that overrides
// Platform/Account (used for workspace expansion).
type Filter struct {
	Platform     string
	Account      string
	Conversation string
	Accounts     []account.Account // from workspace=... expansion
	Kinds        []EventKind       // empty = all kinds
}

func (f Filter) matches(e Event) bool {
	if len(f.Accounts) > 0 {
		found := false
		for _, a := range f.Accounts {
			if a == e.Acct {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	} else {
		if f.Platform != "" && f.Platform != e.Acct.Platform {
			return false
		}
		if f.Account != "" && f.Account != e.Acct.Name {
			return false
		}
	}
	if f.Conversation != "" && f.Conversation != e.Conversation {
		return false
	}
	if len(f.Kinds) > 0 {
		ok := false
		for _, k := range f.Kinds {
			if k == e.Kind {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

type subscriber struct {
	id     uint64
	filter Filter
	ch     chan Event
}

// Broadcast is the in-memory fanout bus. Publish is non-blocking per
// subscriber — a slow consumer drops events (with a log) rather than
// back-pressuring the listener hot path.
type Broadcast struct {
	mu     sync.RWMutex
	subs   map[uint64]*subscriber
	nextID atomic.Uint64
}

func NewBroadcast() *Broadcast {
	return &Broadcast{subs: make(map[uint64]*subscriber)}
}

// Subscribe registers a subscriber with the given filter and buffer size.
// Returns the event channel and a cancel func that unsubscribes and
// closes the channel. The cancel func is idempotent.
func (b *Broadcast) Subscribe(filter Filter, bufSize int) (<-chan Event, func()) {
	if bufSize <= 0 {
		bufSize = 64
	}
	s := &subscriber{
		id:     b.nextID.Add(1),
		filter: filter,
		ch:     make(chan Event, bufSize),
	}

	b.mu.Lock()
	b.subs[s.id] = s
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, s.id)
			b.mu.Unlock()
			close(s.ch)
		})
	}
	return s.ch, cancel
}

// Publish fans an event out to every matching subscriber. Non-blocking:
// if a subscriber's buffer is full, the event is dropped for that
// subscriber and a warning is logged. The listener hot path is never
// blocked by a slow monitor.
func (b *Broadcast) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if !s.filter.matches(e) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			slog.Warn("broadcast subscriber buffer full, event dropped",
				"sub_id", s.id, "kind", e.Kind,
				"account", e.Acct, "conversation", e.Conversation)
		}
	}
}

// Subscribers returns the current count (for diagnostics / tests).
func (b *Broadcast) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
```

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

### Route implementation

```go
func (h *Hub) Route(acct account.Account, conversation string, msg modelv1.MsgLine) RouteResult {
	// 1. Publish to broadcast — cheap, non-blocking, independent of session state.
	h.broadcast.Publish(Event{
		Kind:         EventMessage,
		Ts:           msg.Ts,
		Acct:         acct,
		Conversation: conversation,
		Content:      formatSingleMessage(msg),
		Meta: map[string]any{
			"platform":     acct.Platform,
			"account":      acct.Name,
			"conversation": conversation,
			"kind":         string(EventMessage),
			"msg_id":       msg.ID,
		},
	})

	// 2. Existing per-account session signal path — unchanged.
	key := acct.String()
	h.mu.RLock()
	ch, exists := h.channels[key]
	h.mu.RUnlock()

	if !exists {
		// Still broadcast above; session just isn't bound yet.
		return RouteResult{State: RouteNoSession}
	}

	select {
	case ch.signal <- deliverySignal{kind: signalNewMessage, conversation: conversation}:
	default:
		slog.Error("delivery signal buffer full",
			"account", acct, "conversation", conversation)
	}
	return RouteResult{State: RouteOK}
}
```

`formatSingleMessage` wraps `modelv1.FormatMsg(ResolvedMsg{...}, time.Local)`
to produce the same one-liner shape the session path already emits.
(`FormatMsg` exists at `internal/store/modelv1/format.go:29`.)

### Reaction path

Same pattern in `RouteReaction`: publish to broadcast first, then
signal the session channel. For the broadcast's `Content`, use
`modelv1.FormatReactionFallbackNotification` (no disk lookup) — the
listener hot path stays fast, and the rich parent-message lookup
stays in `deliverReaction` where it already is for session delivery.
Monitor consumers that want richer context can query `pigeon read`
themselves.

## 3. Reply to "does the tail endpoint need state?"

**No.** `/api/tail` is stateless.

The key insight: JSONL files on disk persist across daemon restarts, so
history is always available regardless of when the daemon started. The
broadcast bus only deals with live events from "now" forward. Putting
these together:

```
Client connects:  GET /api/tail?since=2026-04-01T00:00:00Z&workspace=eng

Handler:
  1. Subscribe to broadcast (buffer live events in memory as they arrive)
  2. Resolve filter: workspace → []account, or (platform, account)
  3. Historical replay:
       for each account in filter:
         read.Glob(dir, since) → JSONL files
         store.ReadConversation(...) → parsed, formatted lines
         stream each as `data: {...}`
     Record replayStart = time when step 3 began.
  4. Live drain:
       for event := range subscription:
         if event.Ts < replayStart: skip (already covered by step 3)
         stream as `data: {...}`
  5. On disconnect: unsubscribe, stop.
```

- No server state between connections.
- `since=` can be arbitrarily old; if the files exist, they're streamed.
- Buffer-then-drain in steps 1→4 closes the gap between replay finishing
  and live tailing starting (standard log-tail dedup pattern).
- A monitor dying and restarting just resends `since=`. The server has
  no memory of it.

## 4. Wiring

`Hub.New` constructs the broadcast alongside sessions/channels:

```go
h := &Hub{
    sessions:  make(map[string]*Session),
    channels:  make(map[string]*channel),
    broadcast: NewBroadcast(),
    // ...
}
```

The new SSE handler (`TailHandler`) lives next to the existing
`SSEHandler` in `internal/hub/` and uses `h.broadcast.Subscribe(...)`
plus `h.store` / `read` for historical replay.

## 5. Open questions / deferred

- **Workspace expansion** — do it at the HTTP handler using
  `internal/workspace`, resolving `workspace=foo` to `[]account.Account`
  and populating `Filter.Accounts`. Keeps `Broadcast` platform-agnostic.
- **Dropped event observability** — the non-blocking `Publish` logs on
  drop. For monitor, consider emitting a synthetic `{"_dropped": N}`
  frame so the client notices. Defer until someone hits it.
- **System events** (listener crashed, account reconnected) — same bus,
  `EventSystem` kind, add later. Not needed for v1.
- **Reaction enrichment** — whether monitor should see the parent
  message content inline with a reaction event. Current sketch says no
  (fallback format only). Revisit if consumers need it.
