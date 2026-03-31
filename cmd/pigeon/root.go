package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pigeon",
	Short: "Messaging data CLI for AI agents",
	Long: `pigeon — messaging data CLI for AI agents

Reads locally-stored messaging data (WhatsApp, Slack, etc.) and provides
listeners that receive real-time messages and save them as text files.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		slog.SetDefault(slog.New(tint.NewHandler(os.Stdout, &tint.Options{
			Level:      slog.LevelInfo,
			TimeFormat: time.Kitchen,
		})))
	},
}
