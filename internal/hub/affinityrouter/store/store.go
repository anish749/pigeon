// Package store defines the persistence interface for the affinity router.
// State that must survive process restarts — workstreams, conversation
// affinities, and proposals — flows through this interface.
package store

import "github.com/anish749/pigeon/internal/hub/affinityrouter/models"

// Store persists durable affinity-router state.
type Store interface {
	// LoadWorkstreams returns all persisted workstreams.
	LoadWorkstreams() (map[string]models.Workstream, error)
	// SaveWorkstreams replaces the persisted workstream set.
	SaveWorkstreams(map[string]models.Workstream) error

	// LoadAffinities returns all conversation→workstream affinity weights.
	LoadAffinities() (map[models.ConversationKey][]models.AffinityEntry, error)
	// SaveAffinities replaces the persisted affinity set.
	SaveAffinities(map[models.ConversationKey][]models.AffinityEntry) error

	// LoadProposals returns all persisted proposals and the current sequence counter.
	LoadProposals() ([]*models.Proposal, int, error)
	// SaveProposals replaces the persisted proposal list and sequence counter.
	SaveProposals([]*models.Proposal, int) error
}
