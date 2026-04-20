package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/embedder"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/workspace"
	"github.com/anish749/pigeon/internal/workstream/clients"
	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/reader"
	"github.com/anish749/pigeon/internal/workstream/replay"
	"github.com/anish749/pigeon/internal/workstream/reporter"
)

func newWorkstreamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workstream",
		Short: "Workstream management",
		Long:  "Manage workstream routing and run replay benchmarks.",
	}
	cmd.AddCommand(newWorkstreamReplayCmd())
	cmd.AddCommand(newWorkstreamDiscoverCmd())
	return cmd
}

func newWorkstreamDiscoverCmd() *cobra.Command {
	var sinceStr, untilStr, workspaceFlag, model string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover workstreams from messaging history",
		Long:  "Analyzes signals across conversations to identify distinct ongoing workstreams.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			appCfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			since := time.Now().AddDate(0, 0, -30)
			until := time.Now()
			if sinceStr != "" {
				t, err := time.Parse("2006-01-02", sinceStr)
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				since = t
			}
			if untilStr != "" {
				t, err := time.Parse("2006-01-02", untilStr)
				if err != nil {
					return fmt.Errorf("parse --until: %w", err)
				}
				until = t
			}

			// Resolve which workspaces to discover.
			var workspaces []*workspace.Workspace
			if workspaceFlag != "" {
				ws, err := workspace.GetCurrentWorkspace(appCfg, workspaceFlag)
				if err != nil {
					return err
				}
				workspaces = append(workspaces, ws)
			} else {
				for name := range appCfg.Workspaces {
					ws, err := workspace.GetCurrentWorkspace(appCfg, string(name))
					if err != nil {
						return err
					}
					workspaces = append(workspaces, ws)
				}
				if len(workspaces) == 0 {
					return fmt.Errorf("no workspaces configured — run 'pigeon workspace add' first")
				}
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			for _, ws := range workspaces {
				if err := runDiscover(cmd.Context(), ws, since, until, model, timeout, logger); err != nil {
					return fmt.Errorf("discover workspace %q: %w", ws.Name, err)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sinceStr, "since", "", "Start date (YYYY-MM-DD, default: 30 days ago)")
	cmd.Flags().StringVar(&untilStr, "until", "", "End date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace to discover (default: all workspaces)")
	cmd.Flags().StringVar(&model, "model", "haiku", "Claude model for discovery")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Timeout per LLM call")

	return cmd
}

func runDiscover(ctx context.Context, ws *workspace.Workspace, since, until time.Time, model string, timeout time.Duration, logger *slog.Logger) error {
	root := paths.DefaultDataRoot()
	fsStore := store.NewFSStore(root)

	// Read all signals in the time range.
	signals, err := reader.New(fsStore, root).ReadAll(since, until)
	if err != nil {
		return fmt.Errorf("read signals: %w", err)
	}

	// Filter to this workspace's accounts.
	allowed := make(map[string]bool, len(ws.Accounts))
	for _, a := range ws.Accounts {
		allowed[a.String()] = true
	}
	filtered := signals[:0]
	for _, sig := range signals {
		if allowed[sig.Account.String()] {
			filtered = append(filtered, sig)
		}
	}
	signals = filtered

	fmt.Fprintf(os.Stdout, "Workspace %q: %d signals (%s → %s)\n",
		ws.Name, len(signals), since.Format("2006-01-02"), until.Format("2006-01-02"))

	if len(signals) == 0 {
		fmt.Fprintln(os.Stdout, "  No signals found — nothing to discover.")
		return nil
	}

	claude := clients.New(model, timeout, logger)
	disc := discovery.NewLLMDiscovery(claude, logger)
	discovered, err := disc.Discover(ctx, signals)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}

	printDiscovered(os.Stdout, discovered)
	return nil
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

func newWorkstreamReplayCmd() *cobra.Command {
	cfg := models.DefaultConfig()
	var sinceStr, untilStr, workspaceFlag string
	var skipDiscovery, clearState bool
	var similarityThreshold float64

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay historical data through workstream router",
		Long:  "Reads all historical signals, feeds them through the semantic router, and reports discovered workstreams.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sinceStr != "" {
				t, err := time.Parse("2006-01-02", sinceStr)
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				cfg.Since = t
			}
			if untilStr != "" {
				t, err := time.Parse("2006-01-02", untilStr)
				if err != nil {
					return fmt.Errorf("parse --until: %w", err)
				}
				cfg.Until = t
			}

			appCfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			ws, err := workspace.GetCurrentWorkspace(appCfg, workspaceFlag)
			if err != nil {
				return err
			}
			cfg.Workspace = *ws

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			if clearState {
				storeDir := paths.DefaultDataRoot().Workspace(string(cfg.Workspace.Name)).WorkstreamStore()
				if err := os.RemoveAll(storeDir); err != nil {
					return fmt.Errorf("clear state: %w", err)
				}
				logger.Info("cleared persisted state", "dir", storeDir)
			}

			client, err := embedder.NewClient()
			if err != nil {
				return fmt.Errorf("start embedding sidecar: %w", err)
			}
			defer client.Close()

			report, err := replay.Run(context.Background(), cfg, client, similarityThreshold, skipDiscovery, logger)
			if err != nil {
				return err
			}

			reporter.Print(os.Stdout, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&sinceStr, "since", "", "Start date (YYYY-MM-DD, default: 30 days ago)")
	cmd.Flags().StringVar(&untilStr, "until", "", "End date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Filter to specific workspace")
	cmd.Flags().StringVar(&cfg.Model, "model", "haiku", "Claude model for classification")
	cmd.Flags().DurationVar(&cfg.LLMCallTimeout, "timeout", 60*time.Second, "Timeout per LLM call")
	cmd.Flags().Float64Var(&similarityThreshold, "threshold", 0.4, "Cosine similarity threshold for routing")
	cmd.Flags().BoolVar(&skipDiscovery, "skip-discovery", false, "Skip cold-start discovery, use persisted workstreams")
	cmd.Flags().BoolVar(&clearState, "clear", false, "Delete persisted state before running")

	return cmd
}
