// Package poller runs periodic polls against GWS services.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Poller runs periodic polls against GWS services.
type Poller struct {
	interval    time.Duration
	acct        account.Account
	accountDir  paths.AccountDir
	store       *store.FSStore
	identity    identity.Observer
	syncTracker *syncstatus.Tracker
}

// New creates a Poller with the given interval, account, account directory,
// store instance, and identity observer.
func New(interval time.Duration, acct account.Account, accountDir paths.AccountDir, s *store.FSStore, id identity.Observer, syncTracker *syncstatus.Tracker) *Poller {
	return &Poller{
		interval:    interval,
		acct:        acct,
		accountDir:  accountDir,
		store:       s,
		identity:    id,
		syncTracker: syncTracker,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursors, err := p.store.LoadGWSCursors(p.accountDir)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}

	// Initial poll.
	p.pollAll(ctx, cursors)
	if err := p.store.SaveGWSCursors(p.accountDir, cursors); err != nil {
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
			if err := p.store.SaveGWSCursors(p.accountDir, cursors); err != nil {
				slog.Error("save cursors", "err", err)
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context, cursors *store.GWSCursors) {
	if ctx.Err() != nil {
		return
	}
	p.runAndRecord("gmail", func() (int, error) {
		return PollGmail(p.store, p.accountDir, cursors, p.identity)
	})
	p.runAndRecord("calendar", func() (int, error) {
		return PollCalendar(p.store, p.accountDir, cursors, p.identity)
	})
	p.runAndRecord("drive", func() (int, error) {
		return PollDrive(p.store, p.accountDir, cursors, p.identity)
	})
}

// runAndRecord times a single service poll, logs any error, and appends a
// PollMetric record to the account's poll metrics file. Metric write
// failures are logged but never propagated — telemetry should not break
// the poll loop.
func (p *Poller) runAndRecord(service string, fn func() (int, error)) {
	key := p.acct.Display() + "/" + service
	p.syncTracker.Start(key)
	start := time.Now()
	n, err := fn()
	p.syncTracker.Done(key, err)
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
	if writeErr := appendMetric(p.accountDir.PollMetricsPath(), m); writeErr != nil {
		slog.Error("append poll metric", "service", service, "err", writeErr)
	}
}
