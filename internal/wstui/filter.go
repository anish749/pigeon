package wstui

import (
	"sort"
	"strings"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// filterAndSort returns workstreams scoped to ws, sorted with default
// last and otherwise grouped by state (active, dormant, resolved) then
// alphabetically by name.
//
// An empty workspace selector matches workstreams whose Workspace field
// is also empty — useful for tests and for tooling that operates outside
// a configured workspace.
func filterAndSort(all []models.Workstream, ws config.WorkspaceName) []models.Workstream {
	out := make([]models.Workstream, 0, len(all))
	for _, w := range all {
		if w.Workspace == ws {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Default workstream always last.
		if out[i].IsDefault() != out[j].IsDefault() {
			return !out[i].IsDefault()
		}
		if out[i].State != out[j].State {
			return stateRank(out[i].State) < stateRank(out[j].State)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// stateRank gives the active-first sort order. Unknown states sort last.
func stateRank(s models.WorkstreamState) int {
	switch s {
	case models.StateActive:
		return 0
	case models.StateDormant:
		return 1
	case models.StateResolved:
		return 2
	}
	return 3
}
