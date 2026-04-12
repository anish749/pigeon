package identity

import (
	"fmt"
	"strings"

	"github.com/anish749/pigeon/internal/paths"
)

// Reader merges per-source identity files into a unified view at read time.
// Stateless between calls: each read loads the configured per-source files
// fresh, merges them in memory using stable-identifier matching, and returns
// the merged list.
//
// Because each Reader call builds a fresh local slice and the underlying
// FSStore serialises per-file reads/writes with its own mutex and atomic
// rename, the Reader needs no mutex of its own and is safe to share across
// goroutines.
//
// The Reader is never on a hot path. Slack's UserName (per-message) hits
// the per-workspace Writer, not the Reader — a (workspace, userID) pair
// only ever lives in one file. The Reader is for cold-path cross-source
// name searches (FindUserID, SearchCandidates, People).
type Reader struct {
	store    Store
	dataRoot paths.DataRoot
	dirs     []paths.IdentityDir // if non-nil, limit to these dirs; else discover
}

// NewReader creates a Reader that discovers all service identity dirs under
// the data root at each read.
func NewReader(store Store, dataRoot paths.DataRoot) *Reader {
	return &Reader{store: store, dataRoot: dataRoot}
}

// NewReaderForDirs creates a Reader that only merges the given identity
// dirs. Used for context-scoped lookups.
func NewReaderForDirs(store Store, dirs []paths.IdentityDir) *Reader {
	return &Reader{store: store, dirs: dirs}
}

// load reads all configured identity files and merges them into a single
// deduplicated list of Persons.
func (r *Reader) load() ([]Person, error) {
	dirs := r.dirs
	if dirs == nil {
		discovered, err := r.dataRoot.AllIdentityDirs()
		if err != nil {
			return nil, fmt.Errorf("discover identity dirs: %w", err)
		}
		dirs = discovered
	}

	var merged []Person
	for _, d := range dirs {
		people, err := r.store.LoadPeople(d)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", d.Path(), err)
		}
		for _, p := range people {
			idx := findPersonMatch(merged, p)
			if idx >= 0 {
				merged[idx] = mergePerson(merged[idx], p)
			} else {
				merged = append(merged, p)
			}
		}
	}
	return merged, nil
}

// LookupBySlackID returns the merged person with the given Slack user ID in
// the given workspace, or nil if not found.
func (r *Reader) LookupBySlackID(workspace, userID string) (*Person, error) {
	people, err := r.load()
	if err != nil {
		return nil, err
	}
	for i := range people {
		if ws, ok := people[i].Slack[workspace]; ok && ws.ID == userID {
			p := people[i]
			return &p, nil
		}
	}
	return nil, nil
}

// SearchCandidates returns people matching the trimmed query. If the query
// equals a stable identifier (Slack user ID in any workspace, WhatsApp number,
// or email), at most one person is returned. Otherwise names are matched with
// case-insensitive substring comparison against Person.Name and each Slack
// display name, real name, and username.
func (r *Reader) SearchCandidates(query string) ([]Person, error) {
	q := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if q == "" {
		return nil, nil
	}

	people, err := r.load()
	if err != nil {
		return nil, err
	}

	for i := range people {
		if people[i].matchesAnyExactID(q) {
			p := people[i]
			return []Person{p}, nil
		}
	}

	var out []Person
	for i := range people {
		if people[i].nameMatchesSubstring(q) {
			p := people[i]
			out = append(out, p)
		}
	}
	return out, nil
}

// People returns all known people with cross-source merging applied.
func (r *Reader) People() ([]Person, error) {
	return r.load()
}
