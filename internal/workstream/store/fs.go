package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	if i := slices.IndexFunc(all, func(ws models.Workstream) bool { return ws.ID == id }); i >= 0 {
		return all[i], true, nil
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
	if i := slices.IndexFunc(all, func(w models.Workstream) bool { return w.ID == ws.ID }); i >= 0 {
		all[i] = ws
	} else {
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
	out := slices.DeleteFunc(all, func(w models.Workstream) bool { return w.ID == id })
	if len(out) == len(all) {
		return nil
	}
	return s.save(workstreamsFile, out)
}

func (s *FS) loadWorkstreams() ([]models.Workstream, error) {
	var list []models.Workstream
	if err := s.load(workstreamsFile, &list); err != nil {
		return nil, err
	}
	return list, nil
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
	i := slices.IndexFunc(pf.Proposals, func(p *models.Proposal) bool { return p.ID == id })
	if i < 0 {
		return nil, false, nil
	}
	return pf.Proposals[i], true, nil
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
	before := len(pf.Proposals)
	pf.Proposals = slices.DeleteFunc(pf.Proposals, func(p *models.Proposal) bool { return p.ID == id })
	if len(pf.Proposals) == before {
		return nil
	}
	return s.save(proposalsFile, pf)
}

func (s *FS) PutProposal(p *models.Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pf, err := s.loadProposalFile()
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(pf.Proposals, func(e *models.Proposal) bool { return e.ID == p.ID }); i >= 0 {
		pf.Proposals[i] = p
	} else {
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
