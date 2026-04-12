package daemon

import (
	"context"
	"log/slog"

	"github.com/anish749/pigeon/internal/api"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/lifecycle"
	"github.com/anish749/pigeon/internal/store"
)

// Orchestrator is the daemon-side translator between config and the
// generic lifecycle.Supervisor. It knows which platforms exist and how
// to build a Factory for each config entry; it knows nothing about how
// listeners are restarted, which is the Supervisor's job.
//
// Responsibilities are layered:
//
//	daemon.DaemonRun  →  creates Supervisor and Orchestrator
//	Orchestrator      →  reads config, produces Factories, Reconciles
//	lifecycle.Supervisor →  runs, monitors, and restarts listeners
//	Factory.Build     →  constructs a fresh Listener per incarnation
//	Listener.Run      →  does the actual work
//
// Adding a new platform means writing one factory file (key + build)
// and a single append here — no lifecycle code is duplicated.
type Orchestrator struct {
	sup       *lifecycle.Supervisor
	apiServer *api.Server
	store     *store.FSStore
	identity  *identity.Service
	onMessage hub.MessageNotifyFunc
}

// NewOrchestrator wires an Orchestrator against an existing Supervisor
// and shared dependencies. The Supervisor is owned by the caller (daemon)
// so it can be shut down deterministically at the end of DaemonRun.
func NewOrchestrator(sup *lifecycle.Supervisor, apiServer *api.Server, s *store.FSStore, id *identity.Service, onMessage hub.MessageNotifyFunc) *Orchestrator {
	return &Orchestrator{
		sup:       sup,
		apiServer: apiServer,
		store:     s,
		identity:  id,
		onMessage: onMessage,
	}
}

// Run reconciles the Supervisor against the initial config, then watches
// the config file and re-reconciles on every change. Blocks until ctx
// is cancelled.
func (o *Orchestrator) Run(ctx context.Context, initial *config.Config) {
	o.reconcile(initial)

	for updated := range config.Watch(ctx) {
		slog.InfoContext(ctx, "config changed, reconciling listeners",
			"slack", len(updated.Slack),
			"whatsapp", len(updated.WhatsApp),
			"gws", len(updated.GWS),
			"linear", len(updated.Linear))
		o.reconcile(updated)
	}
}

// reconcile builds the desired set of factories from a config snapshot
// and hands it to the Supervisor. Separated from Run for testability.
func (o *Orchestrator) reconcile(cfg *config.Config) {
	var desired []lifecycle.Factory
	desired = append(desired, slackFactories(cfg.Slack, o.store, o.apiServer, o.identity, o.onMessage)...)
	desired = append(desired, whatsappFactories(cfg.WhatsApp, o.store, o.apiServer, o.identity, o.onMessage)...)
	desired = append(desired, gwsFactories(cfg.GWS, o.store, o.identity)...)
	desired = append(desired, linearFactories(cfg.Linear, o.store)...)
	o.sup.Reconcile(desired)
}

// Supervisor exposes the underlying Supervisor for status/introspection
// callers (e.g. the API server's /status handler).
func (o *Orchestrator) Supervisor() *lifecycle.Supervisor { return o.sup }
