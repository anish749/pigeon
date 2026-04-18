// Package detector defines the ShiftDetector interface for deciding when
// a conversation's buffered signals should be sent to the LLM classifier.
//
// Each ShiftDetector instance is scoped to a single conversation. The router
// creates instances via a Factory and manages the conversation→detector mapping.
package detector

import "github.com/anish749/pigeon/internal/hub/affinityrouter/models"

// ShiftDetector observes incoming signals for a single conversation and
// reports when a shift warrants reclassification. Implementations own
// their internal state (sliding windows, timestamps, embeddings, etc.).
type ShiftDetector interface {
	// Observe records an incoming signal and reports whether a shift has
	// been detected that warrants reclassification.
	Observe(sig models.Signal) bool
}

// Factory creates a new ShiftDetector for a conversation. The factory
// captures shared resources (e.g. embedding model, config) via closure;
// each returned detector is independent and conversation-scoped.
type Factory func() ShiftDetector
