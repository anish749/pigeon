// Package store defines the persistence interface for the affinity router.
// State that must survive process restarts — workstreams, conversation
// affinities, and proposals — flows through this interface.
package store

import "github.com/anish749/pigeon/internal/hub/affinityrouter/models"

// Store persists durable affinity-router state.
type Store interface {
	// GetWorkstream returns a workstream by ID. Returns zero value and false
	// if not found.
	GetWorkstream(id string) (models.Workstream, bool, error)
	// ListWorkstreams returns all workstreams.
	ListWorkstreams() ([]models.Workstream, error)
	// PutWorkstream creates or updates a workstream.
	PutWorkstream(models.Workstream) error

	// GetAffinities returns affinity entries for a conversation.
	// Returns nil (not error) if the conversation has no affinities.
	GetAffinities(models.ConversationKey) ([]models.AffinityEntry, error)
	// PutAffinities replaces the affinity entries for a conversation.
	PutAffinities(models.ConversationKey, []models.AffinityEntry) error

	// ListProposals returns all proposals.
	ListProposals() ([]*models.Proposal, error)
	// PutProposal creates or updates a proposal.
	PutProposal(*models.Proposal) error
	// NextProposalSeq increments and returns the next proposal sequence number.
	NextProposalSeq() (int, error)
}
