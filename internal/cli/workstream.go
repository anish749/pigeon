package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/embedder"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/replay"
	"github.com/anish749/pigeon/internal/workstream/reporter"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/workspace"
)

func newWorkstreamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workstream",
		Short: "Workstream management",
		Long:  "Manage workstream routing and run replay benchmarks.",
	}
	cmd.AddCommand(newWorkstreamReplayCmd())
	return cmd
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
