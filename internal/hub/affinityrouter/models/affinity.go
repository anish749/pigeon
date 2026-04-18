package models

import "time"

// ConversationKey uniquely identifies a conversation within a workspace.
type ConversationKey struct {
	Workspace    string
	Conversation string
}

// AffinityEntry records the strength of a single conversation → workstream
// binding. Immutable — updated by creating a new entry with Record().
type AffinityEntry struct {
	WorkstreamID string
	Strength     int // number of signals routed via this binding
	LastSignal   time.Time
}
