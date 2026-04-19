package replay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/discovery"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/manager"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reader"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/semanticrouter"
	arstore "github.com/anish749/pigeon/internal/hub/affinityrouter/store"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// RunSemantic runs the replay using the semantic router: each signal is
// independently routed by comparing its embedding against workstream focus
// embeddings. No classifier, no affinity cache, no batching.
func RunSemantic(ctx context.Context, cfg Config, embedder semanticrouter.Embedder, threshold float64, skipDiscovery bool, logger *slog.Logger) (*Report, error) {
	startTime := time.Now()

	// Read signals.
	root := paths.DefaultDataRoot()
	fsStore := store.NewFSStore(root)
	rdr := reader.New(fsStore, root)

	logger.Info("reading signals", "since", cfg.Since.Format("2006-01-02"), "until", cfg.Until.Format("2006-01-02"))

	signals, err := rdr.ReadAll(cfg.Since, cfg.Until)
	if err != nil {
		return nil, fmt.Errorf("read signals: %w", err)
	}
	logger.Info("signals loaded", "count", len(signals))
	if len(signals) == 0 {
		return &Report{Since: cfg.Since, Until: cfg.Until}, nil
	}

	// Filter to workspace.
	{
		allowed := make(map[string]bool, len(cfg.Workspace.Accounts))
		for _, a := range cfg.Workspace.Accounts {
			allowed[a.String()] = true
		}
		var filtered []models.Signal
		for _, sig := range signals {
			if allowed[sig.Account.String()] {
				filtered = append(filtered, sig)
			}
		}
		signals = filtered
		logger.Info("filtered to workspace", "workspace", string(cfg.Workspace.Name), "count", len(signals))
	}

	// Set up components.
	storeDir := root.Workspace(string(cfg.Workspace.Name)).AffinityRouter()
	st := arstore.NewFS(storeDir)
	claude := clients.New(cfg.Model, cfg.LLMCallTimeout, logger)
	sc := manager.NewStatCollector()
	mgr := manager.New(claude, sc, cfg, st, logger)
	wsName := cfg.Workspace.Name

	if err := mgr.EnsureDefaultWorkstream(wsName, signals[0].Ts); err != nil {
		return nil, fmt.Errorf("ensure default workstream: %w", err)
	}

	// Discovery or load persisted workstreams.
	if skipDiscovery {
		active, err := st.ActiveWorkstreams()
		if err != nil {
			return nil, fmt.Errorf("load persisted workstreams: %w", err)
		}
		logger.Info("skipped discovery, loaded persisted workstreams", "count", len(active))
	} else {
		disc := discovery.NewLLMDiscovery(claude, logger)
		discovered, err := disc.Discover(ctx, signals)
		if err != nil {
			return nil, fmt.Errorf("cold-start discovery: %w", err)
		}
		for _, dws := range discovered {
			_, err := mgr.ProposeNew(ctx, dws.Name, dws.Focus, wsName, signals[0].Ts)
			if err != nil {
				return nil, fmt.Errorf("create discovered workstream %q: %w", dws.Name, err)
			}
		}
	}

	// Build semantic router with workstream focus embeddings.
	active, err := st.ActiveWorkstreams()
	if err != nil {
		return nil, fmt.Errorf("list active workstreams: %w", err)
	}
	sr := semanticrouter.New(embedder, threshold, models.DefaultWorkstreamID(wsName), logger)
	if err := sr.LoadWorkstreams(ctx, active); err != nil {
		return nil, fmt.Errorf("load workstream embeddings: %w", err)
	}
	logger.Info("semantic router ready", "workstreams", len(active), "threshold", threshold)

	// Route each signal independently — no LLM calls, just embedding comparisons.
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

	// Build report.
	report := &Report{
		Since:        cfg.Since,
		Until:        cfg.Until,
		TotalSignals: len(signals),
		ByType:       countByType(signals),
		ManagerStats: mgr.Stats(),
		Duration:     time.Since(startTime),
	}

	proposals, _ := st.ListProposals()
	for _, p := range proposals {
		report.ProposalsTotal++
		switch p.State {
		case models.ProposalApproved:
			report.ProposalsApproved++
		case models.ProposalRejected:
			report.ProposalsRejected++
		case models.ProposalPending:
			report.ProposalsPending++
		}
	}

	allWS, _ := st.ListWorkstreams()
	for _, ws := range allWS {
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
