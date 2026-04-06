// Package poller runs periodic polls against GWS services.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws/gwsstore"
)

// Poller runs periodic polls against GWS services.
type Poller struct {
	interval    time.Duration
	cursorsPath string
	dataDir     string
}

// New creates a Poller with the given interval, cursors file path, and data directory.
func New(interval time.Duration, cursorsPath, dataDir string) *Poller {
	return &Poller{
		interval:    interval,
		cursorsPath: cursorsPath,
		dataDir:     dataDir,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursors, err := gwsstore.LoadCursors(p.cursorsPath)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}

	// Initial poll.
	p.pollAll(ctx, cursors)
	if err := gwsstore.SaveCursors(p.cursorsPath, cursors); err != nil {
		slog.Error("save cursors", "err", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.pollAll(ctx, cursors)
			if err := gwsstore.SaveCursors(p.cursorsPath, cursors); err != nil {
				slog.Error("save cursors", "err", err)
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context, cursors *gwsstore.Cursors) {
	if ctx.Err() != nil {
		return
	}
	if err := PollCalendar(p.dataDir, cursors); err != nil {
		slog.Error("poll calendar", "err", err)
	}
}
