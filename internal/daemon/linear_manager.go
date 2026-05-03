package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	linearpoller "github.com/anish749/pigeon/internal/linear/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

const linearPollInterval = 30 * time.Second

// LinearManager owns the lifecycle of Linear pollers.
type LinearManager struct {
	store       *store.FSStore
	syncTracker *syncstatus.Tracker
	running     map[string]*runningLinearWorkspace // workspace → cancel
}

type runningLinearWorkspace struct {
	cancel context.CancelFunc
}

// NewLinearManager creates a new LinearManager.
func NewLinearManager(s *store.FSStore, syncTracker *syncstatus.Tracker) *LinearManager {
	return &LinearManager{
		store:       s,
		syncTracker: syncTracker,
		running:     make(map[string]*runningLinearWorkspace),
	}
}

// Run starts pollers for configured Linear workspaces and watches for config changes.
func (m *LinearManager) Run(ctx context.Context, initial []config.LinearConfig) {
	for _, lc := range initial {
		m.startWorkspace(ctx, lc)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Linear)
	}
}

// Count returns the number of running Linear workspaces.
func (m *LinearManager) Count() int {
	return len(m.running)
}

func (m *LinearManager) reconcile(ctx context.Context, desired []config.LinearConfig) {
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

func (m *LinearManager) startWorkspace(ctx context.Context, lc config.LinearConfig) {
	acct := account.New("linear", lc.Workspace)
	acctDir := paths.DefaultDataRoot().AccountFor(acct)

	child, cancel := context.WithCancel(ctx)
	m.running[lc.Workspace] = &runningLinearWorkspace{cancel: cancel}

	go runWithRestart(child, "linear/"+lc.Workspace, func(ctx context.Context) error {
		p := linearpoller.New(linearPollInterval, lc.Workspace, acct, acctDir, m.store, m.syncTracker)
		slog.Info("linear poller started", "workspace", lc.Workspace, "account_dir", acctDir.Path())
		return p.Run(ctx)
	})
}
