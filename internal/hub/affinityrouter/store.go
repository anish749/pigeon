// Package affinityrouter implements workstream-based signal routing.
// The store lives at the package root because it's used by all sub-packages.
package affinityrouter

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Store holds all workstream, affinity, and proposal state in memory.
type Store struct {
	mu          sync.RWMutex
	workstreams map[string]*models.Workstream
	affinities  map[models.ConversationKey]*models.ConversationAffinities
	buffers     map[models.ConversationKey]*models.Buffer
	proposals   []*models.Proposal
	proposalSeq int
}

// NewStore creates an empty workstream store.
func NewStore() *Store {
	return &Store{
		workstreams: make(map[string]*models.Workstream),
		affinities:  make(map[models.ConversationKey]*models.ConversationAffinities),
		buffers:     make(map[models.ConversationKey]*models.Buffer),
	}
}

// --- Workstream CRUD ---

// EnsureDefaultWorkstream creates the default workstream for a workspace if
// it doesn't exist. Returns the default workstream.
func (s *Store) EnsureDefaultWorkstream(workspace string) *models.Workstream {
	id := models.DefaultWorkstreamID(workspace)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ws, ok := s.workstreams[id]; ok {
		return ws
	}
	ws := models.NewDefaultWorkstream(workspace)
	s.workstreams[id] = ws
	return ws
}

// CreateWorkstream adds a new workstream. Returns an error if the ID already exists.
func (s *Store) CreateWorkstream(ws *models.Workstream) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.workstreams[ws.ID]; exists {
		return fmt.Errorf("workstream %q already exists", ws.ID)
	}
	s.workstreams[ws.ID] = ws
	return nil
}

// GetWorkstream returns a workstream by ID, or nil if not found.
func (s *Store) GetWorkstream(id string) *models.Workstream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workstreams[id]
}

// ListWorkstreams returns all workstreams for a workspace. If workspace is
// empty, returns all workstreams across all workspaces.
func (s *Store) ListWorkstreams(workspace string) []*models.Workstream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*models.Workstream
	for _, ws := range s.workstreams {
		if workspace == "" || ws.Workspace == workspace {
			result = append(result, ws)
		}
	}
	return result
}

// ActiveWorkstreams returns non-default, active workstreams for a workspace.
func (s *Store) ActiveWorkstreams(workspace string) []*models.Workstream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*models.Workstream
	for _, ws := range s.workstreams {
		if ws.Workspace == workspace && ws.State == models.StateActive && !ws.IsDefault() {
			result = append(result, ws)
		}
	}
	return result
}

// UpdateWorkstream replaces a workstream in the store.
func (s *Store) UpdateWorkstream(ws *models.Workstream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workstreams[ws.ID] = ws
}

// --- Signal recording (multi-routing) ---

// RecordSignal increments counters on all affiliated workstreams and updates
// conversation affinities. A signal can belong to multiple workstreams.
func (s *Store) RecordSignal(sig models.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := models.ConversationKey{
		Workspace:    sig.Account.Name,
		Conversation: sig.Conversation,
	}

	for _, wsID := range sig.WorkstreamIDs {
		// Update workstream counters.
		if ws, ok := s.workstreams[wsID]; ok {
			ws.SignalCount++
			if sig.Ts.After(ws.LastSignal) {
				ws.LastSignal = sig.Ts
			}
			if sig.Sender != "" && !slices.Contains(ws.Participants, sig.Sender) {
				ws.Participants = append(ws.Participants, sig.Sender)
			}
		}

		// Update conversation affinity for this workstream.
		aff, ok := s.affinities[key]
		if !ok {
			aff = &models.ConversationAffinities{
				Conversation: sig.Conversation,
				Workspace:    sig.Account.Name,
			}
			s.affinities[key] = aff
		}
		aff.Record(wsID, sig.Ts)
	}
}

// --- Affinity queries ---

// GetAffinities returns all workstream affinities for a conversation, or nil.
func (s *Store) GetAffinities(key models.ConversationKey) *models.ConversationAffinities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.affinities[key]
}

// --- Buffer management ---

// BufferSignal adds a signal to the conversation's buffer.
func (s *Store) BufferSignal(sig models.Signal) {
	key := models.ConversationKey{
		Workspace:    sig.Account.Name,
		Conversation: sig.Conversation,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf, ok := s.buffers[key]
	if !ok {
		buf = &models.Buffer{Key: key}
		s.buffers[key] = buf
	}
	buf.Signals = append(buf.Signals, sig)
}

// DrainBuffer removes and returns all buffered signals for a conversation,
// and marks the last classification time.
func (s *Store) DrainBuffer(key models.ConversationKey, now time.Time) []models.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf, ok := s.buffers[key]
	if !ok {
		return nil
	}
	signals := buf.Signals
	buf.Signals = nil
	buf.LastClassified = now
	return signals
}

// ReadyBuffers returns conversation keys whose buffers have reached the
// classification threshold.
func (s *Store) ReadyBuffers(threshold models.BatchThreshold, now time.Time) []models.ConversationKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ready []models.ConversationKey
	for key, buf := range s.buffers {
		if buf.ShouldClassify(threshold, now) {
			ready = append(ready, key)
		}
	}
	return ready
}

// --- Proposal management ---

// AddProposal queues a proposal for user review.
func (s *Store) AddProposal(p *models.Proposal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proposalSeq++
	p.ID = fmt.Sprintf("p-%d", s.proposalSeq)
	p.State = models.ProposalPending
	p.ProposedAt = time.Now()
	s.proposals = append(s.proposals, p)
}

// PendingProposals returns all proposals that haven't been resolved.
func (s *Store) PendingProposals() []*models.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var pending []*models.Proposal
	for _, p := range s.proposals {
		if p.State == models.ProposalPending {
			pending = append(pending, p)
		}
	}
	return pending
}

// ResolveProposal marks a proposal as approved or rejected.
func (s *Store) ResolveProposal(id string, state models.ProposalState) *models.Proposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.proposals {
		if p.ID == id {
			p.State = state
			p.ResolvedAt = time.Now()
			return p
		}
	}
	return nil
}

// AllProposals returns all proposals (for reporting).
func (s *Store) AllProposals() []*models.Proposal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*models.Proposal, len(s.proposals))
	copy(result, s.proposals)
	return result
}

// --- Stats ---

// Stats returns summary statistics for the store.
type Stats struct {
	Workstreams      int
	NonDefault       int
	TotalSignals     int
	Conversations    int
	PendingProposals int
}

// Stats returns summary statistics.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{
		Workstreams:   len(s.workstreams),
		Conversations: len(s.affinities),
	}
	for _, ws := range s.workstreams {
		st.TotalSignals += ws.SignalCount
		if !ws.IsDefault() {
			st.NonDefault++
		}
	}
	for _, p := range s.proposals {
		if p.State == models.ProposalPending {
			st.PendingProposals++
		}
	}
	return st
}
