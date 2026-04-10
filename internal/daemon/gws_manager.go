package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

const gwsPollInterval = 20 * time.Second

// GWSManager owns the lifecycle of GWS pollers.
type GWSManager struct {
	store   *store.FSStore
	running map[string]*runningGWSAccount // email → account
}

type runningGWSAccount struct {
	cancel context.CancelFunc
}

// NewGWSManager creates a new GWSManager. The store is shared with the rest
// of the daemon so that GWS persistence uses the same per-file locks and
// filesystem layout as messaging.
func NewGWSManager(s *store.FSStore) *GWSManager {
	return &GWSManager{
		store:   s,
		running: make(map[string]*runningGWSAccount),
	}
}

// Run starts pollers for configured GWS accounts and watches for config changes.
func (m *GWSManager) Run(ctx context.Context, initial []config.GWSConfig) {
	for _, g := range initial {
		m.startAccount(ctx, g)
	}

	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.GWS)
	}
}

// Count returns the number of running GWS accounts.
func (m *GWSManager) Count() int {
	return len(m.running)
}

func (m *GWSManager) reconcile(ctx context.Context, desired []config.GWSConfig) {
	desiredEmails := make(map[string]config.GWSConfig)
	for _, g := range desired {
		desiredEmails[g.Email] = g
	}

	for email, acct := range m.running {
		if _, ok := desiredEmails[email]; !ok {
			slog.Info("gws account removed, stopping", "email", email)
			acct.cancel()
			delete(m.running, email)
		}
	}

	for _, g := range desired {
		if _, ok := m.running[g.Email]; !ok {
			m.startAccount(ctx, g)
		}
	}
}

func (m *GWSManager) startAccount(ctx context.Context, g config.GWSConfig) {
	acctDir := paths.DefaultDataRoot().AccountFor(account.New("gws", g.Email))

	child, cancel := context.WithCancel(ctx)
	m.running[g.Email] = &runningGWSAccount{cancel: cancel}

	p := poller.New(gwsPollInterval, acctDir, m.store)
	go func() {
		slog.Info("gws poller started", "email", g.Email, "account_dir", acctDir.Path())
		if err := p.Run(child); err != nil && child.Err() == nil {
			slog.Error("gws poller exited", "email", g.Email, "error", err)
		}
	}()
}
