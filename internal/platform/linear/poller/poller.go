// Package poller runs periodic polls against the Linear API via the linear CLI.
package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// Poller runs periodic polls against the Linear API via the linear CLI.
type Poller struct {
	interval    time.Duration
	workspace   string
	acct        account.Account
	accountDir  paths.AccountDir
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
}

// New creates a Poller that syncs issues for the given workspace.
func New(interval time.Duration, workspace string, acct account.Account, accountDir paths.AccountDir, s *store.FSStore, syncTracker *syncstatus.Tracker) *Poller {
	return &Poller{
		interval:    interval,
		workspace:   workspace,
		acct:        acct,
		accountDir:  accountDir,
		store:       s,
		syncTracker: syncTracker,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	cursors, err := p.store.LoadLinearCursors(p.accountDir)
	if err != nil {
		return fmt.Errorf("load cursors: %w", err)
	}

	// Initial poll.
	p.poll(ctx, cursors)
	if err := p.store.SaveLinearCursors(p.accountDir, cursors); err != nil {
		slog.Error("save linear cursors", "err", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.poll(ctx, cursors)
			if err := p.store.SaveLinearCursors(p.accountDir, cursors); err != nil {
				slog.Error("save linear cursors", "err", err)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context, cursors *store.LinearCursors) {
	if ctx.Err() != nil {
		return
	}
	p.syncTracker.Start(p.acct.Display(), syncstatus.KindPoll)
	n, err := PollIssues(ctx, p.store, p.accountDir, p.workspace, cursors)
	p.syncTracker.Done(p.acct.Display(), err)
	if err != nil {
		slog.Error("poll linear issues", "workspace", p.workspace, "err", err)
	} else if n > 0 {
		slog.Info("poll linear issues", "workspace", p.workspace, "changes", n)
	}
}
