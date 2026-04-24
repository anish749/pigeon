package hub

import (
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// EventKind distinguishes event types on the broadcast bus and /api/tail stream.
type EventKind string

const (
	EventMessage  EventKind = "message"
	EventReaction EventKind = "reaction" // reaction added
	EventUnreact  EventKind = "unreact"  // reaction removed
	// EventSystem is used by the tail handler for out-of-band signals
	// (connection ready, replay errors). Not published through the bus —
	// the handler writes it directly to the response.
	EventSystem EventKind = "system"
)

// The Envelope, NotifMsg, NotifReact, and NotifSystem types below define
// the wire shape of every JSON frame emitted by /api/tail. The schema
// documentation in internal/cli/monitor.go (Long help text) and
// docs/monitor-fanout-sketch.md mirrors these struct definitions.
// When you add, remove, or rename a field here — or change a json tag
// on an embedded type such as modelv1.MsgLine or modelv1.ReactLine —
// update both those documents to match. Agents rely on the help text
// for the field list; drift will silently produce wrong filters.

// Envelope carries the routing metadata common to message and reaction
// notifications. It embeds account.Account so that Platform and Name are
// promoted as top-level JSON fields.
type Envelope struct {
	Kind            EventKind `json:"kind"`
	account.Account           // adds "platform" and "name" to the JSON output
	Conversation    string    `json:"conversation,omitempty"`
}

// Notification is a type that can be published to the broadcast bus and
// serialized to the /api/tail stream.
type Notification interface {
	envelope() Envelope
}

// NotifMsg is a message notification. The payload is the MsgLine exactly
// as written to disk; fields are flattened at the JSON level via embedding.
type NotifMsg struct {
	Envelope
	modelv1.MsgLine
}

func (n NotifMsg) envelope() Envelope { return n.Envelope }

// NotifReact is a reaction notification. The payload is the ReactLine
// exactly as written to disk; fields are flattened via embedding.
type NotifReact struct {
	Envelope
	modelv1.ReactLine
}

func (n NotifReact) envelope() Envelope { return n.Envelope }

// NotifSystem is a system-level notification written by the tail handler.
// It carries no account/conversation routing — it's a signal to the
// connected client, not a data event. Kept as a separate type so its
// JSON output doesn't emit empty platform/name fields.
type NotifSystem struct {
	Kind    EventKind `json:"kind"`
	Ts      time.Time `json:"ts"`
	Content string    `json:"content"`
}

// Filter selects which notifications a subscriber receives. An empty
// Accounts slice means "any account"; a non-empty slice is an allowlist.
type Filter struct {
	Accounts []account.Account
}

func (f Filter) matches(env Envelope) bool {
	if len(f.Accounts) == 0 {
		return true
	}
	return slices.Contains(f.Accounts, env.Account)
}

type subscriber struct {
	id     uint64
	filter Filter
	ch     chan Notification
}

// Broadcast is the in-memory fanout bus. Publish is non-blocking per
// subscriber — a slow consumer drops events rather than back-pressuring
// the listener hot path.
type Broadcast struct {
	mu     sync.RWMutex
	subs   map[uint64]*subscriber
	nextID atomic.Uint64
}

func NewBroadcast() *Broadcast {
	return &Broadcast{subs: make(map[uint64]*subscriber)}
}

// Subscribe registers a subscriber with the given filter and buffer size.
// Returns the notification channel and a cancel func that unsubscribes and
// closes the channel. The cancel func is idempotent.
func (b *Broadcast) Subscribe(filter Filter, bufSize int) (<-chan Notification, func()) {
	s := &subscriber{
		id:     b.nextID.Add(1),
		filter: filter,
		ch:     make(chan Notification, bufSize),
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

// Publish fans a notification out to every matching subscriber. Non-blocking:
// if a subscriber's buffer is full the notification is dropped for that
// subscriber and a warning is logged. The listener hot path is never blocked
// by a slow monitor.
func (b *Broadcast) Publish(n Notification) {
	env := n.envelope()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		if !s.filter.matches(env) {
			continue
		}
		select {
		case s.ch <- n:
		default:
			slog.Warn("broadcast subscriber buffer full, notification dropped",
				"sub_id", s.id, "kind", env.Kind,
				"account", env.Account, "conversation", env.Conversation)
		}
	}
}
