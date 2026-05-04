package daemon

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/platform/gws"
	"github.com/anish749/pigeon/internal/platform/gws/poller"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

const gwsPollInterval = 20 * time.Second

// GWSManager owns the lifecycle of GWS pollers.
type GWSManager struct {
	apiServer   *api.Server
	store       *store.FSStore
	idStore     identity.Store
	dataRoot    paths.DataRoot
	syncTracker *syncstatus.Tracker
	running     map[string]*runningGWSAccount // email → account
}

type runningGWSAccount struct {
	cancel context.CancelFunc
	// cfg is the config the poller was started with. reconcile compares
	// this against the desired config to detect any change (env, label,
	// or future fields) and restart the poller when it differs.
	cfg config.GWSConfig
}

// NewGWSManager creates a new GWSManager. The store is shared with the rest
// of the daemon so that GWS persistence uses the same per-file locks and
// filesystem layout as messaging.
//
// Each GWS account gets its own identity.Writer scoped to
// gws/<email-slug>/identity/people.jsonl.
func NewGWSManager(apiServer *api.Server, s *store.FSStore, idStore identity.Store, dataRoot paths.DataRoot, syncTracker *syncstatus.Tracker) *GWSManager {
	return &GWSManager{
		apiServer:   apiServer,
		store:       s,
		idStore:     idStore,
		dataRoot:    dataRoot,
		syncTracker: syncTracker,
		running:     make(map[string]*runningGWSAccount),
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
	desiredByEmail := make(map[string]config.GWSConfig)
	for _, g := range desired {
		desiredByEmail[g.Email] = g
	}

	// Stop accounts that are no longer desired.
	for email, running := range m.running {
		if _, ok := desiredByEmail[email]; !ok {
			slog.Info("gws account removed, stopping", "email", email)
			running.cancel()
			m.apiServer.UnregisterGWS(account.New("gws", email))
			delete(m.running, email)
		}
	}

	// Start new accounts, and restart existing ones whose config has changed.
	// reflect.DeepEqual compares the whole GWSConfig struct, so any future
	// field addition is covered automatically without updating this diff.
	for _, g := range desired {
		running, ok := m.running[g.Email]
		if !ok {
			m.startAccount(ctx, g)
			continue
		}
		if !reflect.DeepEqual(running.cfg, g) {
			slog.Info("gws account config changed, restarting", "email", g.Email)
			running.cancel()
			delete(m.running, g.Email)
			m.startAccount(ctx, g)
		}
	}
}

func (m *GWSManager) startAccount(ctx context.Context, g config.GWSConfig) {
	acct := account.New("gws", g.Email)
	acctDir := m.dataRoot.AccountFor(acct)

	child, cancel := context.WithCancel(ctx)
	m.running[g.Email] = &runningGWSAccount{cancel: cancel, cfg: g}
	m.apiServer.RegisterGWS(acct)

	gwsClient := gws.NewClient(g.Env)
	go runWithRestart(child, "gws/"+g.Email, func(ctx context.Context) error {
		writer := identity.NewWriter(m.idStore, acctDir.Identity())
		p := poller.New(gwsPollInterval, acct, acctDir, m.store, writer, m.syncTracker, gwsClient)
		slog.Info("gws poller started", "email", g.Email, "account_dir", acctDir.Path())
		return p.Run(ctx)
	})
}
