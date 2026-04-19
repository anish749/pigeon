// Package reporter formats replay results for human review.
package reporter

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/replay"
)

// Print writes a human-readable benchmark report to w.
func Print(w io.Writer, r *replay.Report) {
	fmt.Fprintf(w, "REPLAY COMPLETE: %s → %s\n",
		r.Since.Format("2006-01-02"),
		r.Until.Format("2006-01-02"),
	)
	fmt.Fprintf(w, "Duration: %s\n", r.Duration.Round(1e6))
	fmt.Fprintf(w, "Signals replayed: %d\n", r.TotalSignals)

	// Signal breakdown by type.
	fmt.Fprintf(w, "\nSignal breakdown:\n")
	for typ, count := range r.ByType {
		fmt.Fprintf(w, "  %-20s %d\n", typ, count)
	}

	// Manager stats.
	fmt.Fprintf(w, "\nStats:\n")
	fmt.Fprintf(w, "  Focus updates:     %d\n", r.ManagerStats.FocusUpdates)

	// Proposal stats.
	if r.ProposalsTotal > 0 {
		fmt.Fprintf(w, "\nProposals:\n")
		fmt.Fprintf(w, "  Total:     %d\n", r.ProposalsTotal)
		fmt.Fprintf(w, "  Approved:  %d\n", r.ProposalsApproved)
		fmt.Fprintf(w, "  Rejected:  %d\n", r.ProposalsRejected)
		fmt.Fprintf(w, "  Pending:   %d\n", r.ProposalsPending)
	}

	// Group workstreams by workspace.
	byWorkspace := make(map[config.WorkspaceName][]replay.WorkstreamReport)
	for _, ws := range r.Workstreams {
		byWorkspace[ws.Workspace] = append(byWorkspace[ws.Workspace], ws)
	}

	// Sort workspace names.
	var workspaces []config.WorkspaceName
	for ws := range byWorkspace {
		workspaces = append(workspaces, ws)
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i] < workspaces[j] })

	for _, workspace := range workspaces {
		wsList := byWorkspace[workspace]

		// Sort: defaults last, then by signal count descending.
		sort.Slice(wsList, func(i, j int) bool {
			if wsList[i].IsDefault != wsList[j].IsDefault {
				return !wsList[i].IsDefault
			}
			return wsList[i].SignalCount > wsList[j].SignalCount
		})

		fmt.Fprintf(w, "\n═══════════════════════════════════════\n")
		fmt.Fprintf(w, "WORKSPACE: %s\n", string(workspace))
		fmt.Fprintf(w, "═══════════════════════════════════════\n")

		nonDefault := 0
		for _, ws := range wsList {
			if !ws.IsDefault {
				nonDefault++
			}
		}
		fmt.Fprintf(w, "Workstreams discovered: %d\n\n", nonDefault)

		for _, ws := range wsList {
			marker := "  "
			if ws.IsDefault {
				marker = "▸ "
			}
			stateEmoji := stateIcon(ws.State)

			fmt.Fprintf(w, "%s%s %s [%s]\n", marker, stateEmoji, ws.Name, ws.ID)
			fmt.Fprintf(w, "    Signals: %d", ws.SignalCount)
			if !ws.Created.IsZero() && !ws.LastSignal.IsZero() {
				fmt.Fprintf(w, "  |  %s → %s",
					ws.Created.Format("Jan 02"),
					ws.LastSignal.Format("Jan 02"),
				)
			}
			fmt.Fprintln(w)

			if ws.Focus != "" {
				fmt.Fprintf(w, "    Focus: %s\n", truncate(ws.Focus, 120))
			}
			if len(ws.Participants) > 0 {
				fmt.Fprintf(w, "    People: %s\n", strings.Join(limitSlice(ws.Participants, 8), ", "))
			}
			fmt.Fprintln(w)
		}
	}
}

func stateIcon(state models.WorkstreamState) string {
	switch state {
	case models.StateActive:
		return "●"
	case models.StateDormant:
		return "◌"
	case models.StateResolved:
		return "✓"
	default:
		return "?"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func limitSlice(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	result := make([]string, max)
	copy(result, s[:max])
	result[max-1] = fmt.Sprintf("... +%d more", len(s)-max+1)
	return result
}
