// Package classifier defines the WorkstreamClassifier interface and provides
// a batch implementation backed by the Claude CLI.
//
// Each WorkstreamClassifier instance is scoped to a single conversation. The router
// creates instances via a Factory and manages the conversation→classifier mapping.
// Implementations own their internal signal window and classification strategy.
package classifier

import (
	"context"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// WorkstreamClassifier buffers incoming signals for a single conversation and
// classifies them into workstreams on demand. Implementations maintain a
// sliding window of signals and track routing decisions for reclassification.
type WorkstreamClassifier interface {
	// Observe buffers a signal and records the routing decision the router made.
	// Called when the detector does not trigger — the router made a fast-path
	// decision (affinity or default) without invoking classification.
	Observe(sig models.Signal, decision models.RoutingDecision)

	// ObserveAndClassify buffers the signal, then runs classification on the
	// full window. Returns signals whose workstream assignment differs from
	// what was previously decided (by the router or a prior classification).
	ObserveAndClassify(ctx context.Context, sig models.Signal,
		account account.Account, conversation string,
		workstreams []models.Workstream, affinityIDs []string) (*Result, error)

	// Buffered returns the number of signals in the window.
	Buffered() int
}

// Result holds the outcome of a classification round.
type Result struct {
	// Routings contains signals whose workstream assignment changed
	// compared to what was previously decided.
	Routings []SignalRouting

	// NewWorkstreamName is set when proposing a new workstream.
	NewWorkstreamName string

	// NewWorkstreamFocus is the proposed focus description for a new workstream.
	NewWorkstreamFocus string
}

// SignalRouting maps a signal to its new workstream assignment.
type SignalRouting struct {
	Signal        models.Signal
	WorkstreamIDs []string
}

// Factory creates a new WorkstreamClassifier for a conversation. The factory
// captures shared resources (e.g. LLM client, config) via closure;
// each returned classifier is independent and conversation-scoped.
type Factory func() WorkstreamClassifier
