package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

const (
	workstreamsFile = "workstreams.json"
	affinitiesFile  = "affinities.json"
	proposalsFile   = "proposals.json"
)

// FS is a file-backed Store. Each state group is a separate JSON file
// under a single directory.
type FS struct {
	dir string
}

// NewFS creates a file-backed store rooted at dir.
// The directory is created on the first write if it doesn't exist.
func NewFS(dir string) *FS {
	return &FS{dir: dir}
}

// --- Workstreams ---

func (s *FS) LoadWorkstreams() (map[string]models.Workstream, error) {
	var list []models.Workstream
	if err := s.load(workstreamsFile, &list); err != nil {
		return nil, err
	}
	m := make(map[string]models.Workstream, len(list))
	for _, ws := range list {
		m[ws.ID] = ws
	}
	return m, nil
}

func (s *FS) SaveWorkstreams(m map[string]models.Workstream) error {
	list := make([]models.Workstream, 0, len(m))
	for _, ws := range m {
		list = append(list, ws)
	}
	return s.save(workstreamsFile, list)
}

// --- Affinities ---

// affinityRecord is the on-disk representation of a single conversation's
// affinity entries. ConversationKey can't be a JSON map key directly.
type affinityRecord struct {
	Key     models.ConversationKey  `json:"key"`
	Entries []models.AffinityEntry  `json:"entries"`
}

func (s *FS) LoadAffinities() (map[models.ConversationKey][]models.AffinityEntry, error) {
	var records []affinityRecord
	if err := s.load(affinitiesFile, &records); err != nil {
		return nil, err
	}
	m := make(map[models.ConversationKey][]models.AffinityEntry, len(records))
	for _, r := range records {
		m[r.Key] = r.Entries
	}
	return m, nil
}

func (s *FS) SaveAffinities(m map[models.ConversationKey][]models.AffinityEntry) error {
	records := make([]affinityRecord, 0, len(m))
	for key, entries := range m {
		records = append(records, affinityRecord{Key: key, Entries: entries})
	}
	return s.save(affinitiesFile, records)
}

// --- Proposals ---

type proposalFile struct {
	Seq       int               `json:"seq"`
	Proposals []*models.Proposal `json:"proposals"`
}

func (s *FS) LoadProposals() ([]*models.Proposal, int, error) {
	var pf proposalFile
	if err := s.load(proposalsFile, &pf); err != nil {
		return nil, 0, err
	}
	return pf.Proposals, pf.Seq, nil
}

func (s *FS) SaveProposals(proposals []*models.Proposal, seq int) error {
	return s.save(proposalsFile, proposalFile{Seq: seq, Proposals: proposals})
}

// --- helpers ---

// load reads a JSON file into dst. Returns nil with zero-value dst if
// the file doesn't exist (first run).
func (s *FS) load(name string, dst any) error {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load %s: %w", name, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

// save writes v as indented JSON to the named file, creating the
// directory if needed. Writes atomically via temp file + rename.
func (s *FS) save(name string, v any) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	data = append(data, '\n')

	dst := filepath.Join(s.dir, name)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename %s: %w", name, err)
	}
	return nil
}
