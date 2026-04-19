package manager

import (
	"sort"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/workstream/models"
)

type statsEntry struct {
	WorkstreamIDs []string
	Sender        string
	Ts            time.Time
}

// StatCollector tracks per-workstream signal counts, participants, and
// last-signal timestamps. It is the only place these stats are derived —
// the Workstream model carries no counters.
type StatCollector struct {
	mu           sync.RWMutex
	entries      []statsEntry
	byWorkstream map[string][]int // workstreamID → entry indices
}

// NewStatCollector creates an empty StatCollector.
func NewStatCollector() *StatCollector {
	return &StatCollector{
		byWorkstream: make(map[string][]int),
	}
}

// Record tracks a routing decision for stats purposes.
func (s *StatCollector) Record(d models.RoutingDecision, sig models.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := len(s.entries)
	s.entries = append(s.entries, statsEntry{
		WorkstreamIDs: d.WorkstreamIDs,
		Sender:        sig.Sender,
		Ts:            d.Ts,
	})
	for _, wsID := range d.WorkstreamIDs {
		s.byWorkstream[wsID] = append(s.byWorkstream[wsID], idx)
	}
}

// SignalCount returns the number of signals routed to a workstream.
func (s *StatCollector) SignalCount(workstreamID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byWorkstream[workstreamID])
}

// Participants returns distinct sender names for signals routed to a workstream.
func (s *StatCollector) Participants(workstreamID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, idx := range s.byWorkstream[workstreamID] {
		seen[s.entries[idx].Sender] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// LastSignal returns the timestamp of the most recent signal routed to a workstream.
func (s *StatCollector) LastSignal(workstreamID string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var latest time.Time
	for _, idx := range s.byWorkstream[workstreamID] {
		if ts := s.entries[idx].Ts; ts.After(latest) {
			latest = ts
		}
	}
	return latest
}
