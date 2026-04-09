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
	p.runAndRecord("gmail", func() (int, error) {
		return PollGmail(p.account, cursors)
	})
	p.runAndRecord("calendar", func() (int, error) {
		return PollCalendar(p.account, cursors)
	})
	p.runAndRecord("drive", func() (int, error) {
		return PollDrive(p.account, cursors)
	})
}

// runAndRecord times a single service poll, logs any error, and appends a
// PollMetric record to the account's poll metrics file. Metric write
// failures are logged but never propagated — telemetry should not break
// the poll loop.
func (p *Poller) runAndRecord(service string, fn func() (int, error)) {
	start := time.Now()
	n, err := fn()
	m := PollMetric{
		Ts:         start.UTC(),
		Service:    service,
		DurationMs: time.Since(start).Milliseconds(),
		Changes:    n,
	}
	if err != nil {
		m.Err = err.Error()
		slog.Error("poll "+service, "err", err)
	}
	if writeErr := appendMetric(p.account.PollMetricsPath(), m); writeErr != nil {
		slog.Error("append poll metric", "service", service, "err", writeErr)
	}
}
