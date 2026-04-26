package commands

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/clients"
	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/reader"
	wsstore "github.com/anish749/pigeon/internal/workstream/store"
)

// RunWorkstreamDiscover discovers workstreams for one or all workspaces by
// reading signals and running LLM-based batch analysis.
func RunWorkstreamDiscover(ctx context.Context, cfg *config.Config, workspaceFlag string, since, until time.Time, model string, logger *slog.Logger, w io.Writer) error {
	var workspaces []*workspace.Workspace
	if workspaceFlag != "" {
		ws, err := workspace.GetCurrentWorkspace(cfg, workspaceFlag)
		if err != nil {
			return err
		}
		workspaces = append(workspaces, ws)
	} else {
		var err error
		workspaces, err = workspace.GetAllWorkspaces(cfg)
		if err != nil {
			return err
		}
		if len(workspaces) == 0 {
			return fmt.Errorf("no workspaces configured — run 'pigeon workspace add' first")
		}
	}

	claude := clients.New(model, logger)
	disc := discovery.NewLLMDiscovery(claude, logger)

	root := paths.DefaultDataRoot()
	fsStore := store.NewFSStore(root)
	r := reader.New(fsStore, root)

	for _, ws := range workspaces {
		if err := discoverWorkspace(ctx, r, disc, ws, since, until, w); err != nil {
			return fmt.Errorf("discover workspace %q: %w", ws.Name, err)
		}
	}
	return nil
}

func discoverWorkspace(ctx context.Context, r *reader.Reader, disc *discovery.LLMDiscovery, ws *workspace.Workspace, since, until time.Time, w io.Writer) error {
	signals, err := r.ReadAccounts(ws.Accounts, since, until)
	if err != nil {
		return fmt.Errorf("read signals: %w", err)
	}

	fmt.Fprintf(w, "Workspace %q: %d signals (%s → %s)\n",
		ws.Name, len(signals), since.Format("2006-01-02"), until.Format("2006-01-02"))

	if len(signals) == 0 {
		fmt.Fprintln(w, "  No signals found — nothing to discover.")
		return nil
	}

	discovered, err := disc.Discover(ctx, signals)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}

	printDiscovered(w, discovered)

	storeDir := paths.DefaultDataRoot().Workspace(string(ws.Name)).WorkstreamStore()
	st := wsstore.NewFS(storeDir.Path())
	saved, err := persistDiscovered(st, ws.Name, discovered)
	if err != nil {
		return fmt.Errorf("persist discovered workstreams: %w", err)
	}
	if saved > 0 {
		fmt.Fprintf(w, "  Persisted %d workstreams to %s.\n", saved, storeDir.Path())
	}
	return nil
}

// persistDiscovered writes discovered workstreams to the per-workspace store.
// Workstreams are keyed by name-derived ID, so re-running discovery
// replaces same-named workstreams in place rather than duplicating them.
func persistDiscovered(st wsstore.Store, ws config.WorkspaceName, discovered []discovery.DiscoveredWorkstream) (int, error) {
	now := time.Now().UTC()
	for _, d := range discovered {
		w := models.Workstream{
			ID:        "ws-" + slug.Make(d.Name),
			Name:      d.Name,
			Workspace: ws,
			State:     models.StateActive,
			Focus:     d.Focus,
			Created:   now,
		}
		if err := st.PutWorkstream(w); err != nil {
			return 0, fmt.Errorf("put %q: %w", d.Name, err)
		}
	}
	return len(discovered), nil
}

func printDiscovered(w io.Writer, discovered []discovery.DiscoveredWorkstream) {
	if len(discovered) == 0 {
		fmt.Fprintln(w, "  No workstreams discovered.")
		return
	}

	fmt.Fprintf(w, "  Discovered %d workstreams:\n\n", len(discovered))
	for _, ws := range discovered {
		fmt.Fprintf(w, "  %s\n", ws.Name)
		fmt.Fprintf(w, "    Focus: %s\n", ws.Focus)
		if len(ws.Conversations) > 0 {
			fmt.Fprintf(w, "    Conversations: %s\n", strings.Join(ws.Conversations, ", "))
		}
		if len(ws.Participants) > 0 {
			fmt.Fprintf(w, "    Participants: %s\n", strings.Join(ws.Participants, ", "))
		}
		fmt.Fprintln(w)
	}
}
