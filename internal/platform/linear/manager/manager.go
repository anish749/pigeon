package manager

import (
	"context"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	"github.com/anish749/pigeon/internal/paths"
	linearpoller "github.com/anish749/pigeon/internal/platform/linear/poller"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

const linearPollInterval = 30 * time.Second

// Manager owns the lifecycle of Linear pollers.
type Manager struct {
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
	running     map[string]*runningWorkspace // workspace → cancel
}

type runningWorkspace struct {
	cancel context.CancelFunc
}

// NewManager creates a new Manager.
func NewManager(s *store.FSStore, syncTracker *syncstatus.Tracker) *Manager {
	return &Manager{
		store:       s,
		syncTracker: syncTracker,
		running:     make(map[string]*runningWorkspace),
	}
}

// Run starts pollers for configured Linear workspaces and watches for config changes.
func (m *Manager) Run(ctx context.Context, initial []config.LinearConfig) {
	for _, lc := range initial {
		m.startWorkspace(ctx, lc)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Linear)
	}
}

// Count returns the number of running Linear workspaces.
func (m *Manager) Count() int {
	return len(m.running)
}

func (m *Manager) reconcile(ctx context.Context, desired []config.LinearConfig) {
	desiredWorkspaces := make(map[string]config.LinearConfig)
	for _, lc := range desired {
		desiredWorkspaces[lc.Workspace] = lc
	}

	for ws, running := range m.running {
		if _, ok := desiredWorkspaces[ws]; !ok {
			slog.Info("linear workspace removed, stopping", "workspace", ws)
			running.cancel()
			delete(m.running, ws)
		}
	}

	for _, lc := range desired {
		if _, ok := m.running[lc.Workspace]; !ok {
			m.startWorkspace(ctx, lc)
		}
	}
}

func (m *Manager) startWorkspace(ctx context.Context, lc config.LinearConfig) {
	acct := account.New("linear", lc.Workspace)
	acctDir := paths.DefaultDataRoot().AccountFor(acct)

	child, cancel := context.WithCancel(ctx)
	m.running[lc.Workspace] = &runningWorkspace{cancel: cancel}

	go daemon.RunWithRestart(child, "linear/"+lc.Workspace, func(ctx context.Context) error {
		p := linearpoller.New(linearPollInterval, lc.Workspace, acct, acctDir, m.store, m.syncTracker)
		slog.Info("linear poller started", "workspace", lc.Workspace, "account_dir", acctDir.Path())
		return p.Run(ctx)
	})
}
