package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

const gwsPollInterval = 20 * time.Second

// GWSManager translates GWS config entries into supervised Listeners. Actual
// restart / backoff / lifetime is handled by the lifecycle.Supervisor.
type GWSManager struct {
	sup      *lifecycle.Supervisor
	store    *store.FSStore
	identity *identity.Service
}

// NewGWSManager creates a new GWSManager. The store is shared with the rest
// of the daemon so that GWS persistence uses the same per-file locks and
// filesystem layout as messaging.
func NewGWSManager(sup *lifecycle.Supervisor, s *store.FSStore, id *identity.Service) *GWSManager {
	return &GWSManager{sup: sup, store: s, identity: id}
}

// Run applies the initial config and then reconciles the supervised set on
// every config change. Blocks until ctx is cancelled.
func (m *GWSManager) Run(ctx context.Context, initial []config.GWSConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.GWS)
	}
}

func (m *GWSManager) reconcile(ctx context.Context, desired []config.GWSConfig) {
	listeners := make([]lifecycle.Listener, 0, len(desired))
	for _, g := range desired {
		listeners = append(listeners, &gwsListener{
			cfg:      g,
			store:    m.store,
			identity: m.identity,
		})
	}
	if err := m.sup.Reconcile(listeners); err != nil {
		slog.ErrorContext(ctx, "gws reconcile failed", "error", err)
	}
}

// gwsListener wraps a single GWS poller in the Listener interface.
type gwsListener struct {
	cfg      config.GWSConfig
	store    *store.FSStore
	identity *identity.Service
}

func (l *gwsListener) ID() string { return "gws/" + l.cfg.Email }

func (l *gwsListener) Run(ctx context.Context) error {
	acctDir := paths.DefaultDataRoot().AccountFor(account.New("gws", l.cfg.Email))
	slog.InfoContext(ctx, "gws poller started", "email", l.cfg.Email, "account_dir", acctDir.Path())
	p := poller.New(gwsPollInterval, acctDir, l.store, l.identity)
	if err := p.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("gws poller %s: %w", l.cfg.Email, err)
	}
	return nil
}
