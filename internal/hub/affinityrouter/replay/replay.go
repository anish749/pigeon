// Package replay implements the historical replay engine for benchmarking
// the workstream routing model against existing data.
package replay

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/classifier"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/detector"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/manager"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reader"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/router"
	arstore "github.com/anish749/pigeon/internal/hub/affinityrouter/store"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// Config is an alias for the shared config — replay uses it directly.
type Config = models.Config

// Report holds the results of a replay run.
type Report struct {
	Since time.Time
	Until time.Time

	TotalSignals int
	ByType       map[models.SignalType]int

	Workstreams []WorkstreamReport

	RouterStats  router.Stats
	ManagerStats manager.Stats

	ProposalsTotal    int
	ProposalsApproved int
	ProposalsRejected int
	ProposalsPending  int

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

// Run executes the replay: reads all historical signals, feeds them through
// the routing model, and returns a benchmark report.
func Run(ctx context.Context, cfg Config, detectorFactory detector.Factory, logger *slog.Logger) (*Report, error) {
	startTime := time.Now()

	// Read signals.
	root := paths.DefaultDataRoot()
	fsStore := store.NewFSStore(root)
	rdr := reader.New(fsStore, root)

	logger.Info("reading signals",
		"since", cfg.Since.Format("2006-01-02"),
		"until", cfg.Until.Format("2006-01-02"),
	)

	signals, err := rdr.ReadAll(cfg.Since, cfg.Until)
	if err != nil {
		return nil, fmt.Errorf("read signals: %w", err)
	}

	logger.Info("signals loaded", "count", len(signals))

	if len(signals) == 0 {
		return &Report{Since: cfg.Since, Until: cfg.Until}, nil
	}

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
		logger.Info("filtered to workspace", "workspace", string(cfg.Workspace.Name), "accounts", len(allowed), "count", len(signals))
	}

	// Set up components.
	claude := clients.New(cfg.Model, cfg.LLMCallTimeout, logger)
	classifierFactory := classifier.NewBatchFactory(claude, logger)
	sc := manager.NewStatCollector()

	storeDir := filepath.Join(root.Path(), ".affinityrouter")
	st := arstore.NewFS(storeDir)

	rtr, err := router.New(detectorFactory, classifierFactory, cfg, st, logger)
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}
	mgr, err := manager.New(claude, sc, cfg, st, logger)
	if err != nil {
		return nil, fmt.Errorf("create manager: %w", err)
	}

	// Replay: for each signal, route → observe (manager records + manages).
	wsName := cfg.Workspace.Name
	for i, sig := range signals {
		if err := mgr.EnsureDefaultWorkstream(wsName, sig.Ts); err != nil {
			return nil, fmt.Errorf("ensure default workstream: %w", err)
		}

		// Route the signal.
		active := mgr.ActiveWorkstreams(wsName)
		result, err := rtr.Route(ctx, sig, active)
		if err != nil {
			logger.Warn("route failed", "error", err, "index", i)
			continue
		}

		// If the classifier proposed a new workstream, ask the manager.
		if result.NewWorkstreamName != "" {
			newID, err := mgr.ProposeNew(ctx,
				result.NewWorkstreamName,
				result.NewWorkstreamFocus,
				wsName,
				[]models.Signal{sig},
			)
			if err != nil {
				logger.Warn("propose new workstream failed", "error", err)
			} else if newID != "" {
				result.Decision.WorkstreamIDs = append(result.Decision.WorkstreamIDs, newID)
			}
		}

		// Ensure decision has at least the default workstream.
		if len(result.Decision.WorkstreamIDs) == 0 {
			result.Decision.WorkstreamIDs = []string{models.DefaultWorkstreamID(wsName)}
		}

		// Manager records to ledger and runs lifecycle checks.
		if err := mgr.ObserveRouting(ctx, sig, result.Decision); err != nil {
			logger.Warn("manager observe failed", "error", err, "index", i)
		}

		// Re-deliver reclassified signals to the manager.
		for _, routing := range result.Reclassified {
			if routing.Signal.ID == sig.ID {
				continue // already handled above
			}
			reclassDecision := models.RoutingDecision{
				SignalID:      routing.Signal.ID,
				WorkstreamIDs: routing.WorkstreamIDs,
				Ts:            routing.Signal.Ts,
			}
			if err := mgr.ObserveRouting(ctx, routing.Signal, reclassDecision); err != nil {
				logger.Warn("reclassify observe failed", "error", err, "signal", routing.Signal.ID)
			}
		}

		// Update router affinities from the decision.
		key := models.ConversationKey{
			Account:      sig.Account,
			Conversation: sig.Conversation,
		}
		for _, wsID := range result.Decision.WorkstreamIDs {
			if err := rtr.UpdateAffinity(key, wsID, sig.Ts); err != nil {
				logger.Warn("update affinity failed", "error", err)
			}
		}

		// Progress logging.
		if (i+1)%1000 == 0 || i == len(signals)-1 {
			mgrStats := mgr.Stats()
			logger.Info("replay progress",
				"signals", i+1,
				"total", len(signals),
				"workstreams", mgrStats.WorkstreamCount,
			)
		}
	}

	// Build report.
	report := &Report{
		Since:        cfg.Since,
		Until:        cfg.Until,
		TotalSignals: len(signals),
		ByType:       countByType(signals),
		RouterStats:  rtr.Stats(),
		ManagerStats: mgr.Stats(),
		Duration:     time.Since(startTime),
	}

	for _, p := range mgr.AllProposals() {
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

	for _, ws := range mgr.AllWorkstreams() {
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
