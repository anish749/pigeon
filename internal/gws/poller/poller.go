// Package poller runs periodic polls against GWS services.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/gws/gwsstore"
)

// Poller runs periodic polls against GWS services.
type Poller struct {
	interval   time.Duration
	accountDir string // root dir for one account, e.g. ~/.local/share/pigeon/gws/user-at-gmail-com/
}

// New creates a Poller with the given interval and account directory.
// Cursors are stored at {accountDir}/.sync-cursors.yaml.
func New(interval time.Duration, accountDir string) *Poller {
	return &Poller{
		interval:   interval,
		accountDir: accountDir,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursorsPath := filepath.Join(p.accountDir, ".sync-cursors.yaml")
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
	if err := PollCalendar(p.accountDir, cursors); err != nil {
		slog.Error("poll calendar", "err", err)
	}
}
