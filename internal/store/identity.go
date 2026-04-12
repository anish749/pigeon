package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
)

// LoadPeople reads a people.jsonl file into memory. Returns an empty slice
// if the file does not exist (first-run case).
func (s *FSStore) LoadPeople(path paths.PeopleFile) ([]identity.Person, error) {
	p := string(path)
	mu := s.fileMu(p)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()

	var people []identity.Person
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var person identity.Person
		if err := json.Unmarshal(line, &person); err != nil {
			slog.Warn("identity: skipping malformed line",
				"file", p, "line", lineNum, "error", err)
			continue
		}
		people = append(people, person)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return people, nil
}

// SavePeople atomically writes people to a JSONL file (write to temp, rename).
func (s *FSStore) SavePeople(path paths.PeopleFile, people []identity.Person) error {
	p := string(path)
	mu := s.fileMu(p)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	w := bufio.NewWriter(f)
	for _, person := range people {
		data, err := json.Marshal(person)
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

	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
