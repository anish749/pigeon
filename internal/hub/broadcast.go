package hub

import (
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/anish749/pigeon/internal/account"
)

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
