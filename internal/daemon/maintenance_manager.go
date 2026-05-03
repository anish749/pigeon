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
	// (slack post-sync, scheduler) wait for the worker to catch up. That
	// natural backpressure is the goal — silently dropping requests
	// would leave the writer thinking maintenance is queued when it
	// isn't.
	triggerBuffer = 16
)

// MaintenanceManager runs FSStore.Maintain serially across all configured
// accounts. A single worker goroutine consumes a buffered channel; both
// the periodic scheduler and external Trigger calls (e.g. slack post-sync)
// send into the same channel. This guarantees only one Maintain pass is
// in flight at a time across the whole daemon — no parallel rewrites of
// the same files, no startup stampede with many accounts.
//
// Trigger is a blocking channel send: when the buffer is full, the caller
// waits. That gives the trigger source (slack/sync, scheduler) backpressure
// proportional to maintenance throughput.
//
// The scheduler uses the existing `.maintenance.json` mtime (updated by
// every successful Maintain) as the wall-clock anchor for "is this
// account stale?". Wall-clock mtime survives laptop suspend correctly,
// unlike a monotonic ticker.
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
// is full so callers feel backpressure when maintenance can't keep up.
// Slack's post-sync hook is the canonical caller — replaces the previous
// direct store.Maintain(acct) so sync and the periodic loop never race
// on the same files.
func (m *MaintenanceManager) Trigger(acct account.Account) {
	m.requests <- acct
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
	accounts := configuredAccounts(cfg)
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
	slog.Debug("maintenance complete", "account", acct.Display(), "duration", time.Since(started))
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
					m.Trigger(acct)
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

// configuredAccounts returns every account.Account derivable from cfg
// across all platforms. Each per-platform manager iterates its own slice
// of cfg directly; this is the only place in the daemon that needs to
// know the cross-platform shape of Config.
func configuredAccounts(cfg *config.Config) []account.Account {
	var accounts []account.Account
	for _, s := range cfg.Slack {
		accounts = append(accounts, account.New("slack", s.Workspace))
	}
	for _, g := range cfg.GWS {
		accounts = append(accounts, account.New("gws", g.Email))
	}
	for _, w := range cfg.WhatsApp {
		accounts = append(accounts, account.New("whatsapp", w.Account))
	}
	for _, l := range cfg.Linear {
		accounts = append(accounts, account.New("linear", l.Workspace))
	}
	for _, j := range cfg.Jira {
		accounts = append(accounts, j.Account())
	}
	return accounts
}
