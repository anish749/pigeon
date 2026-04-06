// Command gwspoll is a prototype that polls Gmail, Drive, and Calendar
// for changes every 20 seconds via the gws CLI.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/gws/poller"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	cursorsPath := filepath.Join(os.TempDir(), "gwspoll-cursors.yaml")
	slog.Info("starting gws poller", "interval", "20s", "cursors", cursorsPath)

	p := poller.New(20*time.Second, cursorsPath)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := p.Run(ctx); err != nil && err != context.Canceled {
		slog.Error("poller exited", "error", err)
		os.Exit(1)
	}
}
