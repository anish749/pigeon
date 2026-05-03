package daemon

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

const (
	// maintenanceCheckInterval is how often the scheduler wakes to look at
	// each account's last-maintained timestamp. Cheap (one stat per
	// account) and short enough that the daemon catches up quickly after
	// a long laptop suspend without burning CPU on a fast cadence.
	maintenanceCheckInterval = time.Hour

	// maintenanceMinAge is the wall-clock gate: an account is enqueued
	// for maintenance only when its `.maintenance.json` mtime is at
	// least this old. Wall-clock so the gate survives system suspend
	// (Go's monotonic clock does not advance during macOS sleep, so a
	// pure ticker would stay paused across a 28h suspend; reading mtime
	// sidesteps that).
	maintenanceMinAge = 24 * time.Hour

	// triggerBuffer caps how many requests can sit in the queue before
	// Trigger blocks. Sized so the scheduler firing for every configured
	// account in one tick fits without blocking; once it's full, callers
	// wait for the worker to catch up. Backpressure is the goal —
	// dropping requests on overflow would leave the writer thinking
	// maintenance is queued when it isn't.
	triggerBuffer = 16
)

// MaintenanceManager runs FSStore.Maintain serially across all configured
// accounts. A single worker goroutine consumes a buffered channel; both
// the periodic scheduler and external Trigger calls (e.g. slack post-sync)
// send into the same channel. Two structural properties fall out of this
// shape:
//
//   - At most one Maintain pass is in flight across the whole daemon, so
//     parallel rewrites of the same files are impossible by construction.
//   - Trigger is a blocking channel send. When the buffer fills, callers
//     (slack/sync, scheduler) wait for the worker to catch up; silently
//     dropping requests would leave the writer thinking maintenance is
//     queued when it isn't.
//
// The scheduler uses the wall-clock mtime of `.maintenance.json` (which
// FSStore.Maintain bumps on every successful run) as the staleness gate.
// Wall-clock so the gate survives laptop suspend — Go's monotonic clock
// pauses during macOS sleep, which would leave a monotonic-ticker
// scheduler stuck for an entire 28-hour suspend. mtime sidesteps that.
type MaintenanceManager struct {
	store    *store.FSStore
	root     paths.DataRoot
	requests chan account.Account

	// accounts is the live snapshot of configured accounts, refreshed
	// on each config change. atomic so the scheduler reads without
	// locking.
	accounts atomic.Pointer[[]account.Account]
}

// NewMaintenanceManager creates a MaintenanceManager.
func NewMaintenanceManager(s *store.FSStore, root paths.DataRoot) *MaintenanceManager {
	m := &MaintenanceManager{
		store:    s,
		root:     root,
		requests: make(chan account.Account, triggerBuffer),
	}
	empty := []account.Account{}
	m.accounts.Store(&empty)
	return m
}

// Trigger asks the worker to run Maintain for acct. Blocks if the queue
// is full so callers feel backpressure when maintenance can't keep up,
// but returns immediately when ctx is cancelled — daemon shutdown must
// not park slack/sync or the scheduler inside the channel send after
// the worker has exited. Slack's post-sync hook is the canonical
// caller; routing through this method (instead of calling
// FSStore.Maintain directly) keeps eager and periodic compaction
// serialised on the single worker.
func (m *MaintenanceManager) Trigger(ctx context.Context, acct account.Account) {
	select {
	case m.requests <- acct:
	case <-ctx.Done():
	}
}

// Run starts the worker and scheduler, then watches config for changes.
// Blocks until ctx is cancelled.
func (m *MaintenanceManager) Run(ctx context.Context, initial *config.Config) {
	m.setAccounts(initial)

	go m.worker(ctx)
	go m.scheduler(ctx)

	for updated := range config.Watch(ctx) {
		m.setAccounts(updated)
	}
}

func (m *MaintenanceManager) setAccounts(cfg *config.Config) {
	accounts := cfg.AllAccounts()
	m.accounts.Store(&accounts)
}

func (m *MaintenanceManager) snapshotAccounts() []account.Account {
	p := m.accounts.Load()
	if p == nil {
		return nil
	}
	return *p
}

// worker drains the request channel serially. One Maintain at a time
// across the whole daemon.
func (m *MaintenanceManager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case acct := <-m.requests:
			m.runOnce(ctx, acct)
		}
	}
}

func (m *MaintenanceManager) runOnce(ctx context.Context, acct account.Account) {
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	if err := m.store.Maintain(acct); err != nil {
		slog.Error("maintenance failed", "account", acct.Display(), "duration", time.Since(started), "error", err)
		return
	}
	slog.Info("maintenance complete", "account", acct.Display(), "duration", time.Since(started))
}

// scheduler enqueues stale accounts every maintenanceCheckInterval. An
// account is stale when its `.maintenance.json` is missing or older than
// maintenanceMinAge by wall-clock time. Trigger blocks if the queue is
// full, which throttles the scheduler to maintenance throughput.
func (m *MaintenanceManager) scheduler(ctx context.Context) {
	ticker := time.NewTicker(maintenanceCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, acct := range m.snapshotAccounts() {
				if ctx.Err() != nil {
					return
				}
				if m.isStale(acct) {
					m.Trigger(ctx, acct)
				}
			}
		}
	}
}

// isStale reports whether acct's `.maintenance.json` is missing or older
// than maintenanceMinAge. Wall-clock comparison so suspend doesn't skew
// the gate.
func (m *MaintenanceManager) isStale(acct account.Account) bool {
	mf := m.root.AccountFor(acct).MaintenanceFile()
	info, err := os.Stat(mf.Path())
	if err != nil {
		// Missing maintenance state — never been maintained or fresh
		// account post-backfill. Either way, queue it.
		return true
	}
	return time.Since(info.ModTime()) >= maintenanceMinAge
}
