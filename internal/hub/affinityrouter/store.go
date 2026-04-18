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

// Store holds all workstream and affinity state in memory. During replay it
// accumulates state as signals are processed; in production it would be
// persisted to disk.
type Store struct {
	mu          sync.RWMutex
	workstreams map[string]*models.Workstream              // id → workstream
	affinities  map[models.ConversationKey]*models.ConversationAffinity // conversation → affinity
	buffers     map[models.ConversationKey]*models.Buffer  // conversation → pending signals
}

// NewStore creates an empty workstream store.
func NewStore() *Store {
	return &Store{
		workstreams: make(map[string]*models.Workstream),
		affinities:  make(map[models.ConversationKey]*models.ConversationAffinity),
		buffers:     make(map[models.ConversationKey]*models.Buffer),
	}
}

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

// RecordSignal increments counters on the workstream and updates the
// conversation affinity. Called after a signal is routed.
func (s *Store) RecordSignal(sig models.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update workstream counters.
	if ws, ok := s.workstreams[sig.WorkstreamID]; ok {
		ws.SignalCount++
		if sig.Ts.After(ws.LastSignal) {
			ws.LastSignal = sig.Ts
		}
		// Track participant.
		if sig.Sender != "" && !slices.Contains(ws.Participants, sig.Sender) {
			ws.Participants = append(ws.Participants, sig.Sender)
		}
	}

	// Update conversation affinity.
	key := models.ConversationKey{
		Workspace:    sig.Account.Name,
		Conversation: sig.Conversation,
	}
	aff, ok := s.affinities[key]
	if !ok {
		aff = &models.ConversationAffinity{
			Conversation: sig.Conversation,
			Workspace:    sig.Account.Name,
		}
		s.affinities[key] = aff
	}
	aff.WorkstreamID = sig.WorkstreamID
	aff.Strength++
	aff.LastSignal = sig.Ts
}

// GetAffinity returns the current affinity for a conversation, or nil.
func (s *Store) GetAffinity(key models.ConversationKey) *models.ConversationAffinity {
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

// Stats returns summary statistics for the store.
type Stats struct {
	Workstreams    int // total workstreams (including defaults)
	NonDefault     int // workstreams that aren't the default
	TotalSignals   int // total signals across all workstreams
	Conversations  int // conversations with affinities
	PendingBuffers int // buffers with pending signals
}

// Stats returns summary statistics.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{
		Workstreams:    len(s.workstreams),
		Conversations:  len(s.affinities),
		PendingBuffers: len(s.buffers),
	}
	for _, ws := range s.workstreams {
		st.TotalSignals += ws.SignalCount
		if !ws.IsDefault() {
			st.NonDefault++
		}
	}
	return st
}
