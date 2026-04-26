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

// Proposal represents a pending suggestion for a workstream lifecycle
// change. Proposals are an ephemeral queue — Approve converts a proposal
// to a workstream and deletes it; Reject deletes it. No state field is
// needed because every proposal in the store is implicitly pending.
type Proposal struct {
	ID   string       `json:"id"`
	Type ProposalType `json:"type"`

	// For create proposals.
	SuggestedName  string               `json:"suggested_name"`
	SuggestedFocus string               `json:"suggested_focus"`
	Workspace      config.WorkspaceName `json:"workspace"`

	// For merge proposals.
	MergeSourceIDs []string `json:"merge_source_ids,omitempty"`
	MergeTargetID  string   `json:"merge_target_id,omitempty"`

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
