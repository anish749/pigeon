package identity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Service manages cross-source person identities for a single context.
// It is safe for concurrent use by multiple goroutines.
type Service struct {
	filePath string
	mu       sync.Mutex
	people   []Person
	loaded   bool
	dirty    bool
}

// NewService creates an identity service that stores people in the given
// people.jsonl file path.
func NewService(peoplePath string) *Service {
	return &Service{filePath: peoplePath}
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

// People returns a copy of all known people. Loads from disk if needed.
func (s *Service) People() ([]Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.loadLocked(); err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	out := make([]Person, len(s.people))
	copy(out, s.people)
	return out, nil
}

// loadLocked loads people from disk if not already loaded. Must be called
// with s.mu held.
func (s *Service) loadLocked() error {
	if s.loaded {
		return nil
	}

	people, err := loadPeople(s.filePath)
	if err != nil {
		return err
	}
	s.people = people
	s.loaded = true
	return nil
}

// saveLocked atomically writes people to disk. Must be called with s.mu held.
func (s *Service) saveLocked() error {
	if err := savePeople(s.filePath, s.people); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// loadPeople reads a people.jsonl file into memory. Returns an empty slice
// if the file does not exist (first-run case).
func loadPeople(path string) ([]Person, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var people []Person
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var p Person
		if err := json.Unmarshal(line, &p); err != nil {
			slog.Warn("identity: skipping malformed line",
				"file", path, "line", lineNum, "error", err)
			continue
		}
		people = append(people, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return people, nil
}

// savePeople atomically writes people to a JSONL file (write to temp, rename).
func savePeople(path string, people []Person) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	w := bufio.NewWriter(f)
	for _, p := range people {
		data, err := json.Marshal(p)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("marshal person: %w", err)
		}
		w.Write(data)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flush: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
