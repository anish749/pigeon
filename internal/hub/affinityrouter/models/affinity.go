package models

import (
	"time"

	"github.com/anish749/pigeon/internal/account"
)

// ConversationKey uniquely identifies a conversation within an account.
type ConversationKey struct {
	Account      account.Account
	Conversation string
}

// AffinityEntry records the strength of a single conversation → workstream
// binding. Immutable — updated by creating a new entry with Record().
type AffinityEntry struct {
	WorkstreamID string
	Strength     int // number of signals routed via this binding
	LastSignal   time.Time
}
