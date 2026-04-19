// Package discovery implements cold-start workstream discovery.
//
// Unlike the per-conversation classifier which routes individual signals
// to existing workstreams, discovery takes a large batch of signals (100s-1000s)
// across an entire workspace and identifies the distinct ongoing efforts.
//
// This is the same analysis a human would do when looking at a week of
// messages: "what are the workstreams here?" It requires seeing the full
// picture — which conversations are active, who's talking to whom, what
// topics recur across channels and DMs.
package discovery

import (
	"context"

	"github.com/anish749/pigeon/internal/workstream/models"
)

// DiscoveredWorkstream is a workstream found by batch analysis.
type DiscoveredWorkstream struct {
	// Name is a short descriptive name (e.g. "Recommendations Feature").
	Name string

	// Focus is a 1-3 sentence description of what this workstream is about.
	// This becomes the immutable anchor focus for the workstream.
	Focus string

	// Conversations lists the conversation names that contribute to this
	// workstream (e.g. "@alice", "#engineering").
	Conversations []string

	// Participants lists the people involved.
	Participants []string
}

// WorkspaceDiscovery analyzes a batch of signals from a workspace and
// discovers the distinct workstreams present.
type WorkspaceDiscovery interface {
	// Discover takes all signals for a workspace and returns the workstreams
	// it identifies. The signals should span enough time (days to weeks) to
	// capture the full picture of ongoing efforts.
	//
	// Returns a list of discovered workstreams. Signals that don't belong to
	// any workstream (casual chat, one-off questions) are not included —
	// they stay in the default stream.
	Discover(ctx context.Context, signals []models.Signal) ([]DiscoveredWorkstream, error)
}
