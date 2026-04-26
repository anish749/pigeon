package commands

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/clients"
	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/manager"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/reader"
	wsstore "github.com/anish749/pigeon/internal/workstream/store"
)

// RunWorkstreamDiscover discovers workstreams for one or all workspaces by
// reading signals and running LLM-based batch analysis through the manager.
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
	root := paths.DefaultDataRoot()
	r := reader.New(store.NewFSStore(root), root)

	for _, ws := range workspaces {
		if err := discoverWorkspace(ctx, claude, r, ws, since, until, logger, w); err != nil {
			return fmt.Errorf("discover workspace %q: %w", ws.Name, err)
		}
	}
	return nil
}

func discoverWorkspace(ctx context.Context, claude *clients.Client, r *reader.Reader, ws *workspace.Workspace, since, until time.Time, logger *slog.Logger, w io.Writer) error {
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

	storeDir := paths.DefaultDataRoot().Workspace(string(ws.Name)).WorkstreamStore()
	discovered, err := discoverAndPersist(ctx, claude, ws, signals, logger)
	if err != nil {
		return err
	}

	printDiscovered(w, discovered)
	if len(discovered) > 0 {
		fmt.Fprintf(w, "  Persisted to %s.\n", storeDir.Path())
	}
	return nil
}

// DiscoverWorkspace runs the LLM discovery + persistence flow for a
// single workspace and returns the number of workstreams produced. It is
// the headless variant of `pigeon workstream discover` — same pipeline
// (read signals, ensure default, run manager.DiscoverAndPropose), no
// stdout output. Used by the workstream TUI's in-app discovery action,
// which renders its own progress UI.
//
// Returns 0 when the workspace has no signals in the time window — that
// is not an error, just an empty workspace.
func DiscoverWorkspace(ctx context.Context, ws *workspace.Workspace, since, until time.Time, model string, logger *slog.Logger) (int, error) {
	if ws == nil || ws.Name == "" {
		return 0, fmt.Errorf("workspace required")
	}

	root := paths.DefaultDataRoot()
	r := reader.New(store.NewFSStore(root), root)
	signals, err := r.ReadAccounts(ws.Accounts, since, until)
	if err != nil {
		return 0, fmt.Errorf("read signals: %w", err)
	}
	if len(signals) == 0 {
		return 0, nil
	}

	claude := clients.New(model, logger)
	discovered, err := discoverAndPersist(ctx, claude, ws, signals, logger)
	if err != nil {
		return 0, err
	}
	return len(discovered), nil
}

// discoverAndPersist wires up the manager for a single discovery pass
// and returns the discovered workstreams. Shared between the printing
// CLI flow (discoverWorkspace) and the headless TUI flow
// (DiscoverWorkspace).
func discoverAndPersist(ctx context.Context, claude *clients.Client, ws *workspace.Workspace, signals []models.Signal, logger *slog.Logger) ([]discovery.DiscoveredWorkstream, error) {
	storeDir := paths.DefaultDataRoot().Workspace(string(ws.Name)).WorkstreamStore()
	st := wsstore.NewFS(storeDir.Path())
	mgr := manager.New(claude, manager.NewStatCollector(), models.Config{
		ApprovalMode: models.AutoApprove,
		Workspace:    *ws,
	}, st, logger)

	if err := mgr.EnsureDefaultWorkstream(ws.Name, signals[0].Ts); err != nil {
		return nil, fmt.Errorf("ensure default workstream: %w", err)
	}
	return mgr.DiscoverAndPropose(ctx, signals, signals[0].Ts)
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
