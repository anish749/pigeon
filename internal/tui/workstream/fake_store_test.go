package workstream

import (
	"errors"
	"sort"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// testCfg returns a minimal models.Config scoped to ws. Tests that
// need a discovery window override Since/Until on the returned value.
func testCfg(ws config.WorkspaceName) models.Config {
	return models.Config{
		Workspace: workspace.Workspace{Name: ws},
	}
}

// fakeStore is a minimal in-memory store.Store implementation for use
// in tests. It records calls so assertions can verify which method
// fired with which arguments. It is intentionally not safe for
// concurrent use — TUI tests drive it from a single goroutine.
type fakeStore struct {
	workstreams map[string]models.Workstream

	// Recorded calls in order they happened.
	puts    []models.Workstream
	deletes []string

	// putErr/deleteErr, when set, are returned in place of a successful
	// write — used to drive the model's error path.
	putErr    error
	deleteErr error
}

func newFakeStore(seed ...models.Workstream) *fakeStore {
	s := &fakeStore{workstreams: map[string]models.Workstream{}}
	for _, w := range seed {
		s.workstreams[w.ID] = w
	}
	return s
}

func (s *fakeStore) GetWorkstream(id string) (models.Workstream, bool, error) {
	w, ok := s.workstreams[id]
	return w, ok, nil
}

func (s *fakeStore) ListWorkstreams() ([]models.Workstream, error) {
	out := make([]models.Workstream, 0, len(s.workstreams))
	for _, w := range s.workstreams {
		out = append(out, w)
	}
	// Stable order so tests can rely on input ordering for filterAndSort.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStore) RoutableWorkstreams() ([]models.Workstream, error) {
	var routable []models.Workstream
	for _, w := range s.workstreams {
		if !w.IsDefault() {
			routable = append(routable, w)
		}
	}
	return routable, nil
}

func (s *fakeStore) PutWorkstream(w models.Workstream) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.workstreams[w.ID] = w
	s.puts = append(s.puts, w)
	return nil
}

func (s *fakeStore) DeleteWorkstream(id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if _, ok := s.workstreams[id]; ok {
		delete(s.workstreams, id)
		s.deletes = append(s.deletes, id)
	}
	return nil
}

func (s *fakeStore) GetProposal(string) (*models.Proposal, bool, error) { return nil, false, nil }
func (s *fakeStore) ListProposals() ([]*models.Proposal, error)         { return nil, nil }
func (s *fakeStore) PutProposal(*models.Proposal) error                 { return nil }
func (s *fakeStore) DeleteProposal(string) error                        { return nil }
func (s *fakeStore) NextProposalSeq() (int, error)                      { return 0, nil }

// errOnPut returns a configured error to short-circuit PutWorkstream.
// Used by tests that exercise the error-bearing loadedMsg path.
var errOnPut = errors.New("put failed")
