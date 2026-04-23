package hub

import (
	"log/slog"
	"slices"
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
)

// Event is a single notification published to the broadcast bus.
// Content is pre-formatted once at publish time so N subscribers share
// the same formatting work.
type Event struct {
	Kind         EventKind       `json:"kind"`
	Ts           time.Time       `json:"ts"`
	Acct         account.Account `json:"-"`
	Platform     string          `json:"platform"`
	Account      string          `json:"account"`
	Conversation string          `json:"conversation"`
	Content      string          `json:"content"`
	MsgID        string          `json:"msg_id,omitempty"`
}

// Filter selects which events a subscriber receives. An empty Accounts
// slice means "any account"; a non-empty slice is an allowlist.
type Filter struct {
	Accounts []account.Account
}

func (f Filter) matches(e Event) bool {
	if len(f.Accounts) == 0 {
		return true
	}
	return slices.Contains(f.Accounts, e.Acct)
}

type subscriber struct {
	id     uint64
	filter Filter
	ch     chan Event
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
// if a subscriber's buffer is full the event is dropped for that subscriber
// and a warning is logged. The listener hot path is never blocked by a
// slow monitor.
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

// Subscribers returns the current subscriber count (diagnostics / tests).
func (b *Broadcast) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
