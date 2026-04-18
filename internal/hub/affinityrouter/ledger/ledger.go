// Package ledger records routing decisions and derives stats from them.
// It is the single source of truth for signal-to-workstream mappings.
// All stats (signal counts, participants, last signal time per workstream)
// are derived from the ledger, never stored on the Workstream model.
package ledger

import (
	"sort"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// entry pairs a routing decision with minimal signal metadata needed
// for queries like Participants and conversation-based lookups.
type entry struct {
	Decision     models.RoutingDecision
	Sender       string
	Conversation models.ConversationKey
}

// Ledger records routing decisions and derives stats from them.
// It is the single source of truth for signal-to-workstream mappings.
type Ledger struct {
	mu      sync.RWMutex
	entries []entry

	// Indexes for fast lookups.
	bySignal       map[string]int                    // signalID -> entry index
	byWorkstream   map[string][]int                  // workstreamID -> entry indices
	byConversation map[models.ConversationKey][]int   // conversation -> entry indices
}

// New creates an empty Ledger ready to record decisions.
func New() *Ledger {
	return &Ledger{
		bySignal:       make(map[string]int),
		byWorkstream:   make(map[string][]int),
		byConversation: make(map[models.ConversationKey][]int),
	}
}

// Record stores a routing decision alongside minimal signal metadata.
// Called exactly once per signal. The signal is used to extract sender
// and conversation information for derived queries.
func (l *Ledger) Record(d models.RoutingDecision, sig models.Signal) {
	l.mu.Lock()
	defer l.mu.Unlock()

	idx := len(l.entries)
	conv := models.ConversationKey{
		Workspace:    sig.Account.Name,
		Conversation: sig.Conversation,
	}
	l.entries = append(l.entries, entry{
		Decision:     d,
		Sender:       sig.Sender,
		Conversation: conv,
	})

	l.bySignal[d.SignalID] = idx
	for _, wsID := range d.WorkstreamIDs {
		l.byWorkstream[wsID] = append(l.byWorkstream[wsID], idx)
	}
	l.byConversation[conv] = append(l.byConversation[conv], idx)
}

// SignalCount returns the number of signals routed to a workstream.
func (l *Ledger) SignalCount(workstreamID string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.byWorkstream[workstreamID])
}

// Participants returns distinct sender names for signals routed to a workstream,
// sorted alphabetically.
func (l *Ledger) Participants(workstreamID string) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, idx := range l.byWorkstream[workstreamID] {
		sender := l.entries[idx].Sender
		seen[sender] = struct{}{}
	}

	participants := make([]string, 0, len(seen))
	for name := range seen {
		participants = append(participants, name)
	}
	sort.Strings(participants)
	return participants
}

// LastSignal returns the timestamp of the most recent signal routed to a workstream.
// Returns the zero time if no signals have been routed to the workstream.
func (l *Ledger) LastSignal(workstreamID string) time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var latest time.Time
	for _, idx := range l.byWorkstream[workstreamID] {
		ts := l.entries[idx].Decision.Ts
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest
}

// TotalDecisions returns the total number of recorded decisions.
func (l *Ledger) TotalDecisions() int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return len(l.entries)
}
