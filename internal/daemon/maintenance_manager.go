package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/store"
)

// maintenanceInterval is how often the daemon re-runs the compaction pass
// for each configured account. Files are tiny and FSStore.Maintain skips
// unchanged ones via the .maintenance.json mtime cache, so a long cadence
// keeps background work imperceptible. Eager-after-sync compaction inside
// each listener handles the high-activity case; this ticker is the
// quiescent backstop.
const maintenanceInterval = 24 * time.Hour

// MaintenanceManager runs FSStore.Maintain per configured account: once at
// startup, then on a periodic ticker. Without this, only the slack listener
// triggered compaction (eagerly, after each sync), so Linear / Gmail /
// Calendar / Drive / Jira logs accumulated duplicates indefinitely.
//
// Reconciles on config changes the same way every other per-platform
// manager does (LinearManager, JiraManager, …).
type MaintenanceManager struct {
	store   *store.FSStore
	running map[string]*runningMaintenance // key: account.String()
}

type runningMaintenance struct {
	cancel context.CancelFunc
}

// NewMaintenanceManager creates a MaintenanceManager.
func NewMaintenanceManager(s *store.FSStore) *MaintenanceManager {
	return &MaintenanceManager{
		store:   s,
		running: make(map[string]*runningMaintenance),
	}
}

// Run starts maintenance loops for the initial set of accounts and
// reconciles on config changes. Returns when ctx is cancelled.
func (m *MaintenanceManager) Run(ctx context.Context, initial *config.Config) {
	m.reconcile(ctx, initial)

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated)
	}
}

// Count returns the number of running maintenance loops. Used for
// observability in the daemon's startup log.
func (m *MaintenanceManager) Count() int {
	return len(m.running)
}

func (m *MaintenanceManager) reconcile(ctx context.Context, cfg *config.Config) {
	desired := make(map[string]account.Account)
	for _, a := range configuredAccounts(cfg) {
		desired[a.String()] = a
	}

	// Stop loops for accounts that disappeared from config.
	for key, running := range m.running {
		if _, ok := desired[key]; !ok {
			slog.Info("maintenance: account removed, stopping", "account", key)
			running.cancel()
			delete(m.running, key)
		}
	}

	// Start loops for accounts that appeared in config.
	for key, acct := range desired {
		if _, ok := m.running[key]; !ok {
			m.startAccount(ctx, acct)
		}
	}
}

func (m *MaintenanceManager) startAccount(ctx context.Context, acct account.Account) {
	child, cancel := context.WithCancel(ctx)
	m.running[acct.String()] = &runningMaintenance{cancel: cancel}

	go runWithRestart(child, "maintenance/"+acct.String(), func(ctx context.Context) error {
		return m.runLoop(ctx, acct)
	})
}

// runLoop maintains acct on every interval tick plus one immediate pass
// at startup. Errors from a single Maintain call are logged but do not
// break the loop — compaction is best-effort hygiene; readers always
// re-dedup in memory regardless of on-disk state.
func (m *MaintenanceManager) runLoop(ctx context.Context, acct account.Account) error {
	m.runOnce(ctx, acct)

	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
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

// configuredAccounts returns every account.Account derivable from cfg
// across all platforms. Each per-platform manager iterates its own slice
// of cfg directly, so this is the only place in the daemon that needs to
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
