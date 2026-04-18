package models

import (
	"sort"
	"time"
)

// AffinityEntry tracks the strength of a single conversation → workstream binding.
type AffinityEntry struct {
	WorkstreamID string
	Strength     int // number of signals routed via this binding
	LastSignal   time.Time
}

// ConversationAffinities holds all workstream bindings for a conversation.
// A conversation can be affiliated with multiple workstreams simultaneously
// (e.g. an incident channel that feeds both "ES Upgrade" and "Paid vs Organic").
type ConversationAffinities struct {
	Conversation string                    // conversation directory name (e.g. "@alice", "#eng")
	Workspace    string                    // which workspace
	Entries      map[string]*AffinityEntry // workstreamID → entry
}

// WorkstreamIDs returns all affiliated workstream IDs, sorted by strength descending.
func (a *ConversationAffinities) WorkstreamIDs() []string {
	if a == nil || len(a.Entries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.Entries))
	for id := range a.Entries {
		ids = append(ids, id)
	}
	// Sort by strength descending for consistent ordering.
	sort.Slice(ids, func(i, j int) bool {
		return a.Entries[ids[i]].Strength > a.Entries[ids[j]].Strength
	})
	return ids
}

// Record strengthens the affinity to a workstream.
func (a *ConversationAffinities) Record(workstreamID string, ts time.Time) {
	if a.Entries == nil {
		a.Entries = make(map[string]*AffinityEntry)
	}
	entry, ok := a.Entries[workstreamID]
	if !ok {
		entry = &AffinityEntry{WorkstreamID: workstreamID}
		a.Entries[workstreamID] = entry
	}
	entry.Strength++
	if ts.After(entry.LastSignal) {
		entry.LastSignal = ts
	}
}

// ConversationKey uniquely identifies a conversation within a workspace.
type ConversationKey struct {
	Workspace    string
	Conversation string
}

// Buffer holds accumulated signals for a conversation between batch
// classification runs. When the buffer reaches the batch threshold
// (by count or time), the batch classifier is triggered.
type Buffer struct {
	Key            ConversationKey
	Signals        []Signal
	LastClassified time.Time // when the batch classifier last ran for this buffer
}

// BatchThreshold defines when to trigger batch classification.
type BatchThreshold struct {
	MinSignals int           // trigger after this many signals (e.g. 5-10)
	MaxAge     time.Duration // trigger after this much time with pending signals (e.g. 30 min)
}

// ShouldClassify reports whether this buffer has reached the batch threshold.
func (b *Buffer) ShouldClassify(t BatchThreshold, now time.Time) bool {
	if len(b.Signals) == 0 {
		return false
	}
	if len(b.Signals) >= t.MinSignals {
		return true
	}
	if !b.LastClassified.IsZero() && now.Sub(b.LastClassified) >= t.MaxAge {
		return true
	}
	// First batch for this conversation: use the oldest signal's timestamp.
	if b.LastClassified.IsZero() && now.Sub(b.Signals[0].Ts) >= t.MaxAge {
		return true
	}
	return false
}
