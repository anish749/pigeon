// Package models defines immutable data types for the workstream affinity router.
// All types are value types — state changes produce new values, never mutation.
package models

import (
	"time"

	"github.com/anish749/pigeon/internal/account"
)

// SignalType classifies the kind of incoming signal.
type SignalType string

const (
	SignalSlackMessage  SignalType = "slack-message"
	SignalSlackReaction SignalType = "slack-reaction"
	SignalWhatsApp      SignalType = "whatsapp-message"
	SignalEmail         SignalType = "email"
	SignalCalendarEvent SignalType = "calendar-event"
	SignalDriveComment  SignalType = "drive-comment"
	SignalLinearIssue   SignalType = "linear-issue"
	SignalLinearComment SignalType = "linear-comment"
)

// Signal is an immutable incoming event from any platform. It represents
// something that happened — a message, email, calendar event, etc.
// It carries NO routing information. Routing decisions are recorded
// separately in the RoutingLedger.
type Signal struct {
	ID           string          `json:"id"`
	Type         SignalType      `json:"type"`
	Account      account.Account `json:"account"`
	Conversation string          `json:"conversation"`
	ThreadID     string          `json:"thread_id,omitempty"`
	Ts           time.Time       `json:"ts"`
	Sender       string          `json:"sender"`
	Text         string          `json:"text"`
}
