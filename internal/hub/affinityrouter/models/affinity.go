package models

import (
	"time"

	"github.com/anish749/pigeon/internal/account"
)

// ConversationKey uniquely identifies a conversation within an account.
type ConversationKey struct {
	Account      account.Account `json:"account"`
	Conversation string          `json:"conversation"`
}

// AffinityEntry records the strength of a single conversation → workstream
// binding. Immutable — updated by creating a new entry with Record().
type AffinityEntry struct {
	WorkstreamID string    `json:"workstream_id"`
	Strength     int       `json:"strength"`
	LastSignal   time.Time `json:"last_signal"`
}
