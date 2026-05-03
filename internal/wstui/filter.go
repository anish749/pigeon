package wstui

import (
	"sort"
	"strings"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
)

// filterAndSort returns workstreams scoped to ws, sorted with the
// default workstream pinned last and the rest in case-insensitive
// alphabetical order by name.
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
		if out[i].IsDefault() != out[j].IsDefault() {
			return !out[i].IsDefault()
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}
