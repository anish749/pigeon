package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector/embedding"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/replay"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reporter"
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
	var interactive bool
	var burstGap time.Duration
	var detectorType, embedModel string
	var embedWindowSize int
	var embedThreshold float64

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay historical data through workstream router",
		Long:  "Reads all historical signals, feeds them through the routing model, and reports discovered workstreams.",
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

			if interactive {
				cfg.ApprovalMode = models.Interactive
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

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			var factory detector.Factory
			switch detectorType {
			case "burstgap":
				factory = detector.NewBurstGapFactory(burstGap)
			case "cosine":
				socketPath := filepath.Join(paths.StateDir(), "embed.sock")
				client, err := embedding.NewClient(socketPath, embedModel, 2*time.Minute)
				if err != nil {
					return fmt.Errorf("start embedding sidecar: %w", err)
				}
				defer client.Close()
				factory = embedding.NewCosineFactory(client, embedWindowSize, embedThreshold, logger)
			default:
				return fmt.Errorf("unknown detector type: %s (use burstgap or cosine)", detectorType)
			}

			report, err := replay.Run(context.Background(), cfg, factory, logger)
			if err != nil {
				return err
			}

			reporter.Print(os.Stdout, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&sinceStr, "since", "2026-01-18", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&untilStr, "until", "", "End date (YYYY-MM-DD, default: today)")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Filter to specific workspace")
	cmd.Flags().StringVar(&cfg.Model, "model", "haiku", "Claude model for classification")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "Prompt for confirmation on workstream creation")

	// Detector selection.
	cmd.Flags().StringVar(&detectorType, "detector", "burstgap", "Shift detector: burstgap or cosine")
	cmd.Flags().DurationVar(&burstGap, "burst-gap", 90*time.Minute, "Gap duration for burst-gap detector")
	cmd.Flags().StringVar(&embedModel, "embed-model", "all-MiniLM-L6-v2", "Sentence-transformers model for cosine detector")
	cmd.Flags().IntVar(&embedWindowSize, "embed-window", 5, "Sliding window size (messages) for cosine detector")
	cmd.Flags().Float64Var(&embedThreshold, "embed-threshold", 0.6, "Fallback similarity threshold before calibration")

	return cmd
}
