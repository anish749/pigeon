package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anish749/pigeon/internal/identity"
)

// LoadPeople reads a people.jsonl file into memory. Returns an empty slice
// if the file does not exist (first-run case).
func (s *FSStore) LoadPeople(path string) ([]identity.Person, error) {
	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
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
		var p identity.Person
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

// SavePeople atomically writes people to a JSONL file (write to temp, rename).
func (s *FSStore) SavePeople(path string, people []identity.Person) error {
	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
