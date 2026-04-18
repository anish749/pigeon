package models

import "time"

// ConversationAffinity records the binding between a conversation and a
// workstream. For DMs, there is typically one dominant affinity. For channels,
// there may be multiple (different authors/topics route to different workstreams).
type ConversationAffinity struct {
	Conversation string // conversation directory name (e.g. "@alice", "#eng")
	Workspace    string // which workspace
	WorkstreamID string // currently affiliated workstream
	Strength     int    // number of signals routed via this affinity
	LastSignal   time.Time
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
