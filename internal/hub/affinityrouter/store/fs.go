package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	mu  sync.Mutex
}

// NewFS creates a file-backed store rooted at dir.
// The directory is created on the first write if it doesn't exist.
func NewFS(dir string) *FS {
	return &FS{dir: dir}
}

// --- Workstreams ---

func (s *FS) GetWorkstream(id string) (models.Workstream, bool, error) {
	all, err := s.loadWorkstreams()
	if err != nil {
		return models.Workstream{}, false, err
	}
	for _, ws := range all {
		if ws.ID == id {
			return ws, true, nil
		}
	}
	return models.Workstream{}, false, nil
}

func (s *FS) ListWorkstreams() ([]models.Workstream, error) {
	return s.loadWorkstreams()
}

func (s *FS) ActiveWorkstreams() ([]models.Workstream, error) {
	all, err := s.loadWorkstreams()
	if err != nil {
		return nil, err
	}
	var active []models.Workstream
	for _, ws := range all {
		if ws.State == models.StateActive && !ws.IsDefault() {
			active = append(active, ws)
		}
	}
	return active, nil
}

func (s *FS) PutWorkstream(ws models.Workstream) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadWorkstreams()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range all {
		if existing.ID == ws.ID {
			all[i] = ws
			found = true
			break
		}
	}
	if !found {
		all = append(all, ws)
	}
	return s.save(workstreamsFile, all)
}

func (s *FS) loadWorkstreams() ([]models.Workstream, error) {
	var list []models.Workstream
	if err := s.load(workstreamsFile, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// --- Affinities ---

// affinityRecord is the on-disk representation of a single conversation's
// affinity entries. ConversationKey can't be a JSON map key directly.
type affinityRecord struct {
	Key     models.ConversationKey `json:"key"`
	Entries []models.AffinityEntry `json:"entries"`
}

func (s *FS) GetAffinities(key models.ConversationKey) ([]models.AffinityEntry, error) {
	records, err := s.loadAffinities()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Key == key {
			return r.Entries, nil
		}
	}
	return nil, nil
}

func (s *FS) PutAffinities(key models.ConversationKey, entries []models.AffinityEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadAffinities()
	if err != nil {
		return err
	}
	found := false
	for i, r := range records {
		if r.Key == key {
			records[i].Entries = entries
			found = true
			break
		}
	}
	if !found {
		records = append(records, affinityRecord{Key: key, Entries: entries})
	}
	return s.save(affinitiesFile, records)
}

func (s *FS) loadAffinities() ([]affinityRecord, error) {
	var records []affinityRecord
	if err := s.load(affinitiesFile, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// --- Proposals ---

type proposalFile struct {
	Seq       int                `json:"seq"`
	Proposals []*models.Proposal `json:"proposals"`
}

func (s *FS) ListProposals() ([]*models.Proposal, error) {
	pf, err := s.loadProposalFile()
	if err != nil {
		return nil, err
	}
	return pf.Proposals, nil
}

func (s *FS) PutProposal(p *models.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pf, err := s.loadProposalFile()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range pf.Proposals {
		if existing.ID == p.ID {
			pf.Proposals[i] = p
			found = true
			break
		}
	}
	if !found {
		pf.Proposals = append(pf.Proposals, p)
	}
	return s.save(proposalsFile, pf)
}

func (s *FS) NextProposalSeq() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pf, err := s.loadProposalFile()
	if err != nil {
		return 0, err
	}
	pf.Seq++
	if err := s.save(proposalsFile, pf); err != nil {
		return 0, err
	}
	return pf.Seq, nil
}

func (s *FS) loadProposalFile() (proposalFile, error) {
	var pf proposalFile
	if err := s.load(proposalsFile, &pf); err != nil {
		return proposalFile{}, err
	}
	return pf, nil
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
