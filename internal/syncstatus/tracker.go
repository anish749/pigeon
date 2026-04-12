// Package syncstatus tracks in-memory sync state for the daemon status command.
// State is lost on daemon restart — by design.
package syncstatus

import (
	"sync"
	"time"
)

// Info is the JSON-serializable snapshot of one account's sync state.
type Info struct {
	Syncing     bool       `json:"syncing"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type entry struct {
	syncing     bool
	startedAt   time.Time
	completedAt time.Time
	detail      string
	lastErr     string
}

// Tracker is a thread-safe registry of per-account sync state.
type Tracker struct {
	mu      sync.RWMutex
	entries map[string]*entry
}

// NewTracker creates a Tracker.
func NewTracker() *Tracker {
	return &Tracker{entries: make(map[string]*entry)}
}

// Start marks an account as syncing.
func (t *Tracker) Start(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.getOrCreate(key)
	e.syncing = true
	e.startedAt = time.Now()
	e.detail = ""
	e.lastErr = ""
}

// Update sets the progress detail for a syncing account.
func (t *Tracker) Update(key, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.entries[key]; ok {
		e.detail = detail
	}
}

// Done marks a sync as finished. Pass nil for a successful sync.
func (t *Tracker) Done(key string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.getOrCreate(key)
	e.syncing = false
	e.completedAt = time.Now()
	e.detail = ""
	if err != nil {
		e.lastErr = err.Error()
	}
}

// All returns a snapshot of every tracked account's sync state.
func (t *Tracker) All() map[string]Info {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]Info, len(t.entries))
	for k, e := range t.entries {
		info := Info{
			Syncing: e.syncing,
			Detail:  e.detail,
			Error:   e.lastErr,
		}
		if !e.startedAt.IsZero() {
			ts := e.startedAt
			info.StartedAt = &ts
		}
		if !e.completedAt.IsZero() {
			ts := e.completedAt
			info.CompletedAt = &ts
		}
		result[k] = info
	}
	return result
}

func (t *Tracker) getOrCreate(key string) *entry {
	e, ok := t.entries[key]
	if !ok {
		e = &entry{}
		t.entries[key] = e
	}
	return e
}
