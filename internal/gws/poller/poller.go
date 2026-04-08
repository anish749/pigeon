// Package poller runs periodic polls against GWS services.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/paths"
)

// Poller runs periodic polls against GWS services.
type Poller struct {
	interval time.Duration
	account  paths.AccountDir
}

// New creates a Poller with the given interval and account directory.
func New(interval time.Duration, account paths.AccountDir) *Poller {
	return &Poller{
		interval: interval,
		account:  account,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursorsPath := p.account.SyncCursorsPath()
	cursors, err := gwsstore.LoadCursors(cursorsPath)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}

	// Initial poll.
	p.pollAll(ctx, cursors)
	if err := gwsstore.SaveCursors(cursorsPath, cursors); err != nil {
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
			if err := gwsstore.SaveCursors(cursorsPath, cursors); err != nil {
				slog.Error("save cursors", "err", err)
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context, cursors *gwsstore.Cursors) {
	if ctx.Err() != nil {
		return
	}
	if err := PollGmail(p.account, cursors); err != nil {
		slog.Error("poll gmail", "err", err)
	}
	if err := PollCalendar(p.account, cursors); err != nil {
		slog.Error("poll calendar", "err", err)
	}
	if err := PollDrive(p.account, cursors); err != nil {
		slog.Error("poll drive", "err", err)
	}
}
