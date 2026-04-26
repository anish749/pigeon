package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/anish749/pigeon/internal/workstream/models"
)

const (
	workstreamsFile = "workstreams.json"
	proposalsFile   = "proposals.json"
)

// FS is a file-backed Store. Each state group is a separate JSON file
// under a single directory.
type FS struct {
	dir string
	mu  sync.RWMutex
}

// NewFS creates a file-backed store rooted at dir.
// The directory is created on the first write if it doesn't exist.
func NewFS(dir string) *FS {
	return &FS{dir: dir}
}

// --- Workstreams ---

func (s *FS) GetWorkstream(id string) (models.Workstream, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadWorkstreams()
}

func (s *FS) RoutableWorkstreams() ([]models.Workstream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all, err := s.loadWorkstreams()
	if err != nil {
		return nil, err
	}
	var routable []models.Workstream
	for _, ws := range all {
		if !ws.IsDefault() {
			routable = append(routable, ws)
		}
	}
	return routable, nil
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

func (s *FS) DeleteWorkstream(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.loadWorkstreams()
	if err != nil {
		return err
	}
	out := make([]models.Workstream, 0, len(all))
	for _, w := range all {
		if w.ID != id {
			out = append(out, w)
		}
	}
	if len(out) == len(all) {
		return nil
	}
	return s.save(workstreamsFile, out)
}

// workstreamWithLegacyState is the on-disk shape used during the
// state-removal migration: a Workstream plus a transitional `state`
// field. New writes won't include it (the field is gone from the
// canonical model), but existing files persisted before the migration
// still carry it. When a stale "resolved" row is observed we drop it
// (those were merge sources whose target absorbed the focus); other
// values are ignored and the row keeps its other fields.
type workstreamWithLegacyState struct {
	models.Workstream
	State string `json:"state,omitempty"`
}

func (s *FS) loadWorkstreams() ([]models.Workstream, error) {
	var raw []workstreamWithLegacyState
	if err := s.load(workstreamsFile, &raw); err != nil {
		return nil, err
	}
	out := make([]models.Workstream, 0, len(raw))
	rewrite := false
	for _, e := range raw {
		if e.State != "" {
			rewrite = true
		}
		if e.State == "resolved" {
			continue
		}
		out = append(out, e.Workstream)
	}
	if rewrite {
		if err := s.save(workstreamsFile, out); err != nil {
			return nil, fmt.Errorf("migrate workstreams: %w", err)
		}
	}
	return out, nil
}

// --- Proposals ---

type proposalFile struct {
	Seq       int                `json:"seq"`
	Proposals []*models.Proposal `json:"proposals"`
}

func (s *FS) GetProposal(id string) (*models.Proposal, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pf, err := s.loadProposalFile()
	if err != nil {
		return nil, false, err
	}
	for _, p := range pf.Proposals {
		if p.ID == id {
			return p, true, nil
		}
	}
	return nil, false, nil
}

func (s *FS) ListProposals() ([]*models.Proposal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pf, err := s.loadProposalFile()
	if err != nil {
		return nil, err
	}
	return pf.Proposals, nil
}

func (s *FS) DeleteProposal(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pf, err := s.loadProposalFile()
	if err != nil {
		return err
	}
	out := pf.Proposals[:0]
	for _, p := range pf.Proposals {
		if p.ID != id {
			out = append(out, p)
		}
	}
	if len(out) == len(pf.Proposals) {
		return nil
	}
	pf.Proposals = out
	return s.save(proposalsFile, pf)
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
