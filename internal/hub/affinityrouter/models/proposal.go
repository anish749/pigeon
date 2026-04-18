package models

import "time"

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
	ID    string       // unique proposal ID
	Type  ProposalType // create, merge, split
	State ProposalState

	// For create proposals.
	SuggestedName  string // proposed workstream name
	SuggestedFocus string // proposed focus description
	Workspace      string

	// For merge proposals.
	MergeSourceIDs []string // workstream IDs to merge
	MergeTargetID  string   // resulting workstream ID (or new)

	// Context — what triggered this proposal.
	TriggeringSignals []Signal

	// Timestamps
	ProposedAt time.Time
	ResolvedAt time.Time
}

// ApprovalMode controls how proposals are handled during replay.
type ApprovalMode int

const (
	// AutoApprove approves all proposals automatically (baseline benchmarking).
	AutoApprove ApprovalMode = iota

	// Interactive prompts the user via stdin for each proposal.
	Interactive
)
