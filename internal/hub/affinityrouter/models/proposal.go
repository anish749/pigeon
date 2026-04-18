package models

import (
	"time"

	"github.com/anish749/pigeon/internal/config"
)

// ProposalType classifies a workstream lifecycle proposal.
type ProposalType string

const (
	ProposalCreate ProposalType = "create"
	ProposalMerge  ProposalType = "merge"
	ProposalSplit  ProposalType = "split"
)

// ProposalState tracks whether a proposal has been acted on.
type ProposalState string

const (
	ProposalPending  ProposalState = "pending"
	ProposalApproved ProposalState = "approved"
	ProposalRejected ProposalState = "rejected"
)

// Proposal represents a suggested workstream lifecycle change that needs
// user confirmation before taking effect.
type Proposal struct {
	ID    string        `json:"id"`
	Type  ProposalType  `json:"type"`
	State ProposalState `json:"state"`

	// For create proposals.
	SuggestedName  string               `json:"suggested_name"`
	SuggestedFocus string               `json:"suggested_focus"`
	Workspace      config.WorkspaceName `json:"workspace"`

	// For merge proposals.
	MergeSourceIDs []string `json:"merge_source_ids,omitempty"`
	MergeTargetID  string   `json:"merge_target_id,omitempty"`

	// Context — what triggered this proposal.
	TriggeringSignals []Signal `json:"triggering_signals,omitempty"`

	// Timestamps
	ProposedAt time.Time `json:"proposed_at"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
}

// ApprovalMode controls how proposals are handled during replay.
type ApprovalMode int

const (
	// AutoApprove approves all proposals automatically (baseline benchmarking).
	AutoApprove ApprovalMode = iota

	// Interactive prompts the user via stdin for each proposal.
	Interactive
)
