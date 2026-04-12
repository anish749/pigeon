package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/lifecycle"
	linearpoller "github.com/anish749/pigeon/internal/linear/poller"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

const linearPollInterval = 30 * time.Second

// LinearManager translates Linear config entries into supervised Listeners.
type LinearManager struct {
	sup   *lifecycle.Supervisor
	store *store.FSStore
}

// NewLinearManager creates a new LinearManager.
func NewLinearManager(sup *lifecycle.Supervisor, s *store.FSStore) *LinearManager {
	return &LinearManager{sup: sup, store: s}
}

// Run applies the initial config and reconciles on every change.
func (m *LinearManager) Run(ctx context.Context, initial []config.LinearConfig) {
	m.reconcile(ctx, initial)
	for updated := range config.Watch(ctx) {
		m.reconcile(ctx, updated.Linear)
	}
}

func (m *LinearManager) reconcile(ctx context.Context, desired []config.LinearConfig) {
	listeners := make([]lifecycle.Listener, 0, len(desired))
	for _, lc := range desired {
		listeners = append(listeners, &linearListener{cfg: lc, store: m.store})
	}
	if err := m.sup.Reconcile(listeners); err != nil {
		slog.ErrorContext(ctx, "linear reconcile failed", "error", err)
	}
}

type linearListener struct {
	cfg   config.LinearConfig
	store *store.FSStore
}

func (l *linearListener) ID() string { return "linear/" + l.cfg.Workspace }

func (l *linearListener) Run(ctx context.Context) error {
	acctDir := paths.DefaultDataRoot().AccountFor(account.New("linear-issues", l.cfg.Workspace))
	slog.InfoContext(ctx, "linear poller started", "workspace", l.cfg.Workspace, "account_dir", acctDir.Path())
	p := linearpoller.New(linearPollInterval, l.cfg.Workspace, acctDir, l.store)
	if err := p.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("linear poller %s: %w", l.cfg.Workspace, err)
	}
	return nil
}
