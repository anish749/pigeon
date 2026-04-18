package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/replay"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reporter"
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

			report, err := replay.Run(context.Background(), cfg, logger)
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
	cmd.Flags().IntVar(&cfg.BatchMinSignals, "batch-size", 8, "Signals per batch classification")
	cmd.Flags().StringVar(&cfg.Model, "model", "haiku", "Claude model for classification")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "Prompt for confirmation on workstream creation")

	return cmd
}
