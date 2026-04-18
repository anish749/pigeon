// Package classifier defines the WorkstreamClassifier interface and provides
// a batch implementation backed by the Claude CLI.
//
// Each WorkstreamClassifier instance is scoped to a single conversation. The router
// creates instances via a Factory and manages the conversation→classifier mapping.
// Implementations own their internal buffer of signals and classification strategy.
package classifier

import (
	"context"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Result is the classification outcome for a batch of signals.
type Result struct {
	// Signals is the batch of signals that were classified.
	Signals []models.Signal

	// WorkstreamIDs lists existing workstreams these signals belong to.
	// A signal can belong to multiple workstreams (multi-routing).
	WorkstreamIDs []string

	// NewWorkstreamName is set when proposing a new workstream.
	NewWorkstreamName string

	// NewWorkstreamFocus is the proposed focus description for a new workstream.
	NewWorkstreamFocus string
}

// WorkstreamClassifier buffers incoming signals for a single conversation and
// classifies them into workstreams on demand. Implementations own their
// internal buffer and classification strategy.
type WorkstreamClassifier interface {
	// Observe buffers a signal for future classification.
	Observe(sig models.Signal)

	// Classify classifies all buffered signals against the given workstreams
	// and drains the buffer. Returns nil if no signals are buffered.
	Classify(ctx context.Context, key models.ConversationKey, workstreams []models.Workstream, affinityIDs []string) (*Result, error)

	// Buffered returns the number of signals currently buffered.
	Buffered() int
}

// Factory creates a new WorkstreamClassifier for a conversation. The factory
// captures shared resources (e.g. LLM client, config) via closure;
// each returned classifier is independent and conversation-scoped.
type Factory func() WorkstreamClassifier
