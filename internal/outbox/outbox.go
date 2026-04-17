// Package outbox manages a queue of pending outgoing actions that require
// human review before execution. Items are held in memory until approved
// or dismissed.
//
// The Payload field uses json.RawMessage rather than a concrete type to
// keep this package decoupled from the api package (which imports outbox),
// avoiding a circular dependency. The api layer marshals SendRequest into
// the payload on submit and unmarshals it back on approve.
package outbox

import (
	"encoding/json"
	"sync"
	"time"
)

// Item is a single pending outgoing action awaiting review.
type Item struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// SubmitFunc is called when a new item is added to the outbox.
// Implementations must not block; use a goroutine for slow work.
type SubmitFunc func(item *Item)

// Outbox is an in-memory queue of items pending review.
type Outbox struct {
	mu       sync.RWMutex
	items    map[string]*Item
	order    []string // insertion order
	onSubmit SubmitFunc
}

// New creates an empty Outbox.
func New() *Outbox {
	return &Outbox{
		items: make(map[string]*Item),
	}
}

// OnSubmit registers a callback that fires when a new item is submitted.
func (o *Outbox) OnSubmit(fn SubmitFunc) {
	o.mu.Lock()
	o.onSubmit = fn
	o.mu.Unlock()
}

// Submit adds a new item to the outbox and returns it.
func (o *Outbox) Submit(sessionID string, payload json.RawMessage) *Item {
	item := &Item{
		ID:        shortID(),
		SessionID: sessionID,
		Payload:   payload,
		CreatedAt: time.Now(),
	}

	o.mu.Lock()
	o.items[item.ID] = item
	o.order = append(o.order, item.ID)
	fn := o.onSubmit
	o.mu.Unlock()

	if fn != nil {
		fn(item)
	}

	return item
}

// List returns all pending items in insertion order.
func (o *Outbox) List() []*Item {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var result []*Item
	for _, id := range o.order {
		if item, ok := o.items[id]; ok {
			result = append(result, item)
		}
	}
	return result
}

// Get returns a single item by ID, or nil if not found.
func (o *Outbox) Get(id string) *Item {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.items[id]
}

// UpdatePayload replaces the payload of an existing item. Returns false if
// the item does not exist.
func (o *Outbox) UpdatePayload(id string, payload json.RawMessage) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	item, ok := o.items[id]
	if !ok {
		return false
	}
	item.Payload = payload
	return true
}

// Remove deletes an item from the outbox.
func (o *Outbox) Remove(id string) {
	o.mu.Lock()
	if _, ok := o.items[id]; ok {
		delete(o.items, id)
		for i, oid := range o.order {
			if oid == id {
				o.order = append(o.order[:i], o.order[i+1:]...)
				break
			}
		}
	}
	o.mu.Unlock()
}
