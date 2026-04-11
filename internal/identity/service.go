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
	LoadPeople(dir paths.IdentityDir) ([]Person, error)
	SavePeople(dir paths.IdentityDir, people []Person) error
}

// Service manages cross-source person identities for a single context.
// It is safe for concurrent use by multiple goroutines.
type Service struct {
	store  Store
	dir    paths.IdentityDir
	mu     sync.Mutex
	people []Person
	loaded bool
	dirty  bool
}

// NewService creates an identity service that stores people in the given
// identity directory using the provided store for IO.
func NewService(store Store, dir paths.IdentityDir) *Service {
	return &Service{store: store, dir: dir}
}

// Observe processes a single identity signal. The signal is matched against
// existing people by stable identifiers (email, Slack ID, phone) and either
// merged into an existing person or used to create a new one.
//
// Changes are flushed to disk after each call. For bulk signal sources
// (e.g. Slack user sync), prefer ObserveBatch.
func (s *Service) Observe(sig Signal) error {
	return s.ObserveBatch([]Signal{sig})
}

// ObserveBatch processes multiple signals and writes the result to disk once.
// This is the preferred method for bulk signal sources like Slack startup
// (hundreds of users) or WhatsApp contact sync.
func (s *Service) ObserveBatch(signals []Signal) error {
	if len(signals) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	today := time.Now().UTC().Format("2006-01-02")

	for _, sig := range signals {
		idx := findMatch(s.people, sig)
		if idx >= 0 {
			s.people[idx].merge(sig, today)
		} else {
			s.people = append(s.people, newPerson(sig, today))
		}
		s.dirty = true
	}

	if s.dirty {
		if err := s.saveLocked(); err != nil {
			return fmt.Errorf("save identity: %w", err)
		}
	}
	return nil
}

// LookupBySlackID returns the person with the given Slack user ID in the
// given workspace, or nil if not found. Loads from disk if needed.
func (s *Service) LookupBySlackID(workspace, userID string) (*Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	for i := range s.people {
		if ws, ok := s.people[i].Slack[workspace]; ok && ws.ID == userID {
			p := s.people[i]
			return &p, nil
		}
	}
	return nil, nil
}

// SearchCandidates returns people matching the trimmed query. If the query
// equals a stable identifier (Slack user ID in any workspace, WhatsApp number,
// or email), at most one person is returned. Otherwise names are matched with
// case-insensitive substring comparison against Person.Name and each Slack
// display name, real name, and username. Zero, one, or many people may be
// returned for name search; a single name match is still returned as a
// one-element slice.
func (s *Service) SearchCandidates(query string) ([]Person, error) {
	q := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if q == "" {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	for i := range s.people {
		if s.people[i].matchesAnyExactID(q) {
			p := s.people[i]
			return []Person{p}, nil
		}
	}

	var out []Person
	for i := range s.people {
		if s.people[i].nameMatchesSubstring(q) {
			p := s.people[i]
			out = append(out, p)
		}
	}
	return out, nil
}

// loadLocked loads people from disk if not already loaded. Must be called
// with s.mu held.
func (s *Service) loadLocked() error {
	if s.loaded {
		return nil
	}

	people, err := s.store.LoadPeople(s.dir)
	if err != nil {
		return err
	}
	s.people = people
	s.loaded = true
	return nil
}

// saveLocked writes people to disk via the store. Must be called with s.mu held.
func (s *Service) saveLocked() error {
	if err := s.store.SavePeople(s.dir, s.people); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
