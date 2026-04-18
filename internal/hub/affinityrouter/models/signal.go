// Package models defines the data structures for the workstream affinity router.
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

// Signal is the unified representation of an incoming event from any platform.
// It is the atomic unit that gets routed into a workstream.
type Signal struct {
	// Identity
	ID   string     // platform-specific ID
	Type SignalType // what kind of signal

	// Source
	Account      account.Account // platform + workspace
	Conversation string          // channel/DM/email-thread/etc.
	ThreadID     string          // thread within conversation (Slack threads, email threadId)

	// Content
	Ts     time.Time // when the signal occurred
	Sender string    // display name of the sender
	Text   string    // message body, email subject+snippet, event title, etc.

	// Routing (set by the accumulator)
	WorkstreamID string // which workstream this signal is routed to
}
