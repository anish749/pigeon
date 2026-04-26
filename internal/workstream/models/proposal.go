package models

import (
	"time"

	"github.com/anish749/pigeon/internal/config"
)

// Proposal represents a pending suggestion to create a workstream.
// Proposals are an ephemeral queue — Approve converts a proposal to a
// workstream and deletes it; Reject deletes it.
type Proposal struct {
	ID             string               `json:"id"`
	SuggestedName  string               `json:"suggested_name"`
	SuggestedFocus string               `json:"suggested_focus"`
	Workspace      config.WorkspaceName `json:"workspace"`

	// Context — what triggered this proposal.
	TriggeringSignals []Signal `json:"triggering_signals,omitempty"`

	ProposedAt time.Time `json:"proposed_at"`
}

// ApprovalMode controls how proposals are handled during replay.
type ApprovalMode int

const (
	// AutoApprove approves all proposals automatically (baseline benchmarking).
	AutoApprove ApprovalMode = iota

	// Interactive prompts the user via stdin for each proposal.
	Interactive
)
