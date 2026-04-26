// Package replay implements the historical replay engine for benchmarking
// the workstream routing model against existing data.
package replay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/embedder"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/workstream/clients"
	"github.com/anish749/pigeon/internal/workstream/manager"
	"github.com/anish749/pigeon/internal/workstream/models"
	"github.com/anish749/pigeon/internal/workstream/reader"
	"github.com/anish749/pigeon/internal/workstream/router"
	arstore "github.com/anish749/pigeon/internal/workstream/store"
)

// Config is an alias for the shared config.
type Config = models.Config

// Report holds the results of a replay run.
type Report struct {
	Since time.Time
	Until time.Time

	TotalSignals int
	ByType       map[models.SignalType]int

	Workstreams []WorkstreamReport

	ManagerStats manager.Stats

	Duration time.Duration
}

// WorkstreamReport describes a discovered workstream.
type WorkstreamReport struct {
	ID           string
	Name         string
	Workspace    config.WorkspaceName
	State        models.WorkstreamState
	Focus        string
	SignalCount  int
	Participants []string
	Created      time.Time
	LastSignal   time.Time
	IsDefault    bool
}

// Run reads historical signals, routes them through the semantic router,
// and returns a benchmark report.
//
// When skipDiscovery is true, workstreams are loaded from persisted state
// instead of running cold-start discovery.
func Run(ctx context.Context, cfg Config, emb embedder.Embedder, threshold float64, skipDiscovery bool, logger *slog.Logger) (*Report, error) {
	startTime := time.Now()

	// Set up persistence and manager.
	root := paths.DefaultDataRoot()
	storeDir := root.Workspace(string(cfg.Workspace.Name)).WorkstreamStore()
	st := arstore.NewFS(storeDir.Path())
	claude := clients.New(cfg.Model, logger)
	signalReader := reader.New(store.NewFSStore(root), root)
	sc := manager.NewStatCollector()
	mgr := manager.New(claude, signalReader, sc, cfg, st, logger)
	wsName := cfg.Workspace.Name

	// Read workspace-scoped signals through the manager.
	signals, err := mgr.ReadSignals(ctx, cfg.Since, cfg.Until)
	if err != nil {
		return nil, fmt.Errorf("read signals: %w", err)
	}
	logger.Info("signals loaded", "count", len(signals))
	if len(signals) == 0 {
		return &Report{Since: cfg.Since, Until: cfg.Until}, nil
	}

	logger.Info("workspace signals loaded", "workspace", string(cfg.Workspace.Name), "count", len(signals))

	// Discovery or load persisted workstreams.
	if skipDiscovery {
		if err := mgr.EnsureDefaultWorkstream(wsName, signals[0].Ts); err != nil {
			return nil, fmt.Errorf("ensure default workstream: %w", err)
		}
		active, err := st.ActiveWorkstreams()
		if err != nil {
			return nil, fmt.Errorf("load persisted workstreams: %w", err)
		}
		logger.Info("skipped discovery, loaded persisted workstreams", "count", len(active))
	} else {
		if _, err := mgr.DiscoverAndProposeSignals(ctx, signals); err != nil {
			return nil, fmt.Errorf("cold-start discovery: %w", err)
		}
	}

	// Build semantic router from active workstreams.
	active, err := st.ActiveWorkstreams()
	if err != nil {
		return nil, fmt.Errorf("list active workstreams: %w", err)
	}
	sr := router.New(emb, threshold, models.DefaultWorkstreamID(wsName), logger)
	if err := sr.LoadWorkstreams(ctx, active); err != nil {
		return nil, fmt.Errorf("load workstream embeddings: %w", err)
	}
	logger.Info("semantic router ready", "workstreams", len(active), "threshold", threshold)

	// Route each signal independently.
	for i, sig := range signals {
		decision, err := sr.Route(ctx, sig)
		if err != nil {
			logger.Warn("route failed", "error", err, "index", i)
			continue
		}
		if err := mgr.ObserveRouting(ctx, sig, decision); err != nil {
			logger.Warn("manager observe failed", "error", err, "index", i)
		}
		if (i+1)%500 == 0 || i == len(signals)-1 {
			logger.Info("replay progress", "signals", i+1, "total", len(signals))
		}
	}

	return buildReport(cfg, signals, startTime, mgr, sc, st)
}

func buildReport(cfg Config, signals []models.Signal, startTime time.Time, mgr *manager.Manager, sc *manager.StatCollector, st arstore.Store) (*Report, error) {
	report := &Report{
		Since:        cfg.Since,
		Until:        cfg.Until,
		TotalSignals: len(signals),
		ByType:       countByType(signals),
		ManagerStats: mgr.Stats(),
		Duration:     time.Since(startTime),
	}

	workstreams, err := st.ListWorkstreams()
	if err != nil {
		return nil, fmt.Errorf("list workstreams: %w", err)
	}
	for _, ws := range workstreams {
		report.Workstreams = append(report.Workstreams, WorkstreamReport{
			ID:           ws.ID,
			Name:         ws.Name,
			Workspace:    ws.Workspace,
			State:        ws.State,
			Focus:        ws.Focus,
			SignalCount:  sc.SignalCount(ws.ID),
			Participants: sc.Participants(ws.ID),
			Created:      ws.Created,
			LastSignal:   sc.LastSignal(ws.ID),
			IsDefault:    ws.IsDefault(),
		})
	}

	return report, nil
}

func countByType(signals []models.Signal) map[models.SignalType]int {
	counts := make(map[models.SignalType]int)
	for _, sig := range signals {
		counts[sig.Type]++
	}
	return counts
}
