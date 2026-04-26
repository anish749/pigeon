// Package store defines the persistence interface for the affinity router.
// State that must survive process restarts — workstreams and proposals —
// flows through this interface.
package store

import "github.com/anish749/pigeon/internal/workstream/models"

// Store persists durable affinity-router state.
type Store interface {
	// GetWorkstream returns a workstream by ID. Returns zero value and false
	// if not found.
	GetWorkstream(id string) (models.Workstream, bool, error)
	// ListWorkstreams returns all workstreams.
	ListWorkstreams() ([]models.Workstream, error)
	// ActiveWorkstreams returns non-default workstreams in the active state.
	ActiveWorkstreams() ([]models.Workstream, error)
	// PutWorkstream creates or updates a workstream.
	PutWorkstream(models.Workstream) error

	// GetProposal returns a proposal by ID. Returns nil and false if not found.
	GetProposal(id string) (*models.Proposal, bool, error)
	// ListProposals returns all proposals.
	ListProposals() ([]*models.Proposal, error)
	// PutProposal creates or updates a proposal.
	PutProposal(*models.Proposal) error
	// NextProposalSeq increments and returns the next proposal sequence number.
	NextProposalSeq() (int, error)
}
