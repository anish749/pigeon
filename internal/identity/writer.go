package identity

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// Store is the persistence interface for identity data.
type Store interface {
	LoadPeople(path paths.PeopleFile) ([]Person, error)
	SavePeople(path paths.PeopleFile, people []Person) error
}

// Writer owns identity observations for a single source (one platform +
// one account). Signals are matched/merged against this source's own people
// only — cross-source merging happens at read time via Reader.
//
// A Writer is safe for concurrent use by multiple goroutines.
type Writer struct {
	store  Store
	path   paths.PeopleFile
	mu     sync.Mutex
	people []Person
	loaded bool
	dirty  bool
}

// NewWriter creates a Writer that persists this source's people file via the
// given store.
func NewWriter(store Store, dir paths.IdentityDir) *Writer {
	return &Writer{store: store, path: dir.PeopleFile()}
}

// Observe processes a single signal. Prefer ObserveBatch for bulk sources
// (Slack sync, WhatsApp contact load) — each call flushes to disk.
func (w *Writer) Observe(sig Signal) error {
	return w.ObserveBatch([]Signal{sig})
}

// ObserveBatch processes multiple signals and writes once. Within this
// writer's own people, signals are merged by stable identifier
// (email, Slack ID, phone); otherwise a new person is appended.
func (w *Writer) ObserveBatch(signals []Signal) error {
	if len(signals) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.loadLocked(); err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	today := time.Now().UTC().Format("2006-01-02")

	for _, sig := range signals {
		idx := findMatch(w.people, sig)
		if idx >= 0 {
			w.people[idx].merge(sig, today)
		} else {
			w.people = append(w.people, newPerson(sig, today))
		}
		w.dirty = true
	}

	if w.dirty {
		if err := w.saveLocked(); err != nil {
			return fmt.Errorf("save identity: %w", err)
		}
	}
	return nil
}

// LookupBySlackID returns the person with the given Slack user ID in the
// given workspace, or nil if not found. This is used by the Slack resolver's
// hot path (UserName on every incoming message): a (workspace, userID) pair
// only ever exists in its own workspace's file, so consulting the merged
// Reader would add nothing here.
func (w *Writer) LookupBySlackID(workspace, userID string) (*Person, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.loadLocked(); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	for i := range w.people {
		if ws, ok := w.people[i].Slack[workspace]; ok && ws.ID == userID {
			p := w.people[i]
			return &p, nil
		}
	}
	return nil, nil
}

// SearchCandidates returns people in this source matching the trimmed query.
// If the query equals a stable identifier (Slack user ID, email, or phone),
// at most one person is returned. Otherwise names are matched
// case-insensitively.
func (w *Writer) SearchCandidates(query string) ([]Person, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.loadLocked(); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	q := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if q == "" {
		return nil, nil
	}

	for i := range w.people {
		if w.people[i].matchesAnyExactID(q) {
			p := w.people[i]
			return []Person{p}, nil
		}
	}

	var out []Person
	for i := range w.people {
		if w.people[i].nameMatchesSubstring(q) {
			p := w.people[i]
			out = append(out, p)
		}
	}
	return out, nil
}

// loadLocked loads people from disk if not already loaded. Must be called
// with w.mu held.
func (w *Writer) loadLocked() error {
	if w.loaded {
		return nil
	}
	people, err := w.store.LoadPeople(w.path)
	if err != nil {
		return err
	}
	w.people = people
	w.loaded = true
	return nil
}

// saveLocked atomically writes people to disk. Must be called with w.mu held.
func (w *Writer) saveLocked() error {
	if err := w.store.SavePeople(w.path, w.people); err != nil {
		return err
	}
	w.dirty = false
	return nil
}
