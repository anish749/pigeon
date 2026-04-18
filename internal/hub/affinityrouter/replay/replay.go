// Package replay implements the historical replay engine for benchmarking
// the workstream routing model against existing data.
package replay

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/accumulator"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/classifier"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/manager"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/reader"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// Config holds the replay configuration.
type Config struct {
	// Time range for the replay.
	Since time.Time
	Until time.Time

	// Batch classification thresholds.
	BatchMinSignals int           // default: 8
	BatchMaxAge     time.Duration // default: 30m

	// Focus update interval — how many signals between focus refreshes.
	FocusUpdateInterval int // default: 50

	// Claude model to use.
	Model string // default: "haiku"

	// Claude CLI timeout per call.
	Timeout time.Duration // default: 60s

	// Workspace filter — if set, only replay this workspace.
	Workspace string

	// ApprovalMode controls how workstream proposals are handled.
	ApprovalMode models.ApprovalMode
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Since:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:               time.Now(),
		BatchMinSignals:     8,
		BatchMaxAge:         30 * time.Minute,
		FocusUpdateInterval: 50,
		Model:               "haiku",
		Timeout:             60 * time.Second,
	}
}

// Report holds the results of a replay run.
type Report struct {
	// Time range replayed.
	Since time.Time
	Until time.Time

	// Signal counts.
	TotalSignals int
	ByType       map[models.SignalType]int

	// Workstream results.
	Workstreams []WorkstreamReport

	// Routing stats.
	AccumulatorStats accumulator.AccumulatorStats
	ManagerStats     manager.ManagerStats

	// Proposals
	ProposalsTotal    int
	ProposalsApproved int
	ProposalsRejected int
	ProposalsPending  int

	// Cost estimates.
	ClassifierCalls int
	FocusUpdates    int
	Duration        time.Duration
}

// WorkstreamReport describes a discovered workstream.
type WorkstreamReport struct {
	ID           string
	Name         string
	Workspace    string
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
func Run(ctx context.Context, cfg Config, logger *slog.Logger) (*Report, error) {
	startTime := time.Now()

	// Set up the data reader.
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

	// Filter by workspace if requested.
	if cfg.Workspace != "" {
		var filtered []models.Signal
		for _, sig := range signals {
			if sig.Account.Name == cfg.Workspace {
				filtered = append(filtered, sig)
			}
		}
		signals = filtered
		logger.Info("filtered to workspace", "workspace", cfg.Workspace, "count", len(signals))
	}

	// Set up the routing components.
	wsStore := affinityrouter.NewStore()
	claude := clients.New(cfg.Model, cfg.Timeout, logger)
	batchClassifier := classifier.New(claude, logger)
	threshold := models.BatchThreshold{
		MinSignals: cfg.BatchMinSignals,
		MaxAge:     cfg.BatchMaxAge,
	}
	acc := accumulator.New(wsStore, batchClassifier, threshold, cfg.ApprovalMode, logger)
	mgr := manager.New(wsStore, claude, logger)

	// Track signals per workstream for focus updates.
	signalsSinceUpdate := make(map[string]int)

	// Replay signals chronologically.
	for i, sig := range signals {
		if err := acc.Receive(ctx, sig); err != nil {
			logger.Warn("receive signal failed", "error", err, "index", i)
			continue
		}

		// Periodically update focus for active workstreams.
		for _, wsID := range sig.WorkstreamIDs {
			signalsSinceUpdate[wsID]++
		}
		for wsID, count := range signalsSinceUpdate {
			if count >= cfg.FocusUpdateInterval {
				ws := wsStore.GetWorkstream(wsID)
				if ws != nil && !ws.IsDefault() {
					var recent []models.Signal
					start := max(i-cfg.FocusUpdateInterval, 0)
					for j := start; j <= i; j++ {
						if slices.Contains(signals[j].WorkstreamIDs, wsID) {
							recent = append(recent, signals[j])
						}
					}
					if _, err := mgr.UpdateFocus(ctx, ws, recent); err != nil {
						logger.Warn("focus update failed", "workstream", wsID, "error", err)
					}
				}
				signalsSinceUpdate[wsID] = 0
			}
		}

		// Progress logging.
		if (i+1)%1000 == 0 || i == len(signals)-1 {
			stats := wsStore.Stats()
			logger.Info("replay progress",
				"signals", i+1,
				"total", len(signals),
				"workstreams", stats.NonDefault,
				"conversations", stats.Conversations,
			)
		}
	}

	// Flush remaining buffers.
	if len(signals) > 0 {
		lastSig := signals[len(signals)-1]
		if err := acc.ProcessPending(ctx, lastSig); err != nil {
			logger.Warn("flush pending failed", "error", err)
		}
	}

	// Build report.
	report := &Report{
		Since:            cfg.Since,
		Until:            cfg.Until,
		TotalSignals:     len(signals),
		ByType:           countByType(signals),
		AccumulatorStats: acc.Stats(),
		ManagerStats:     mgr.Stats(),
		ClassifierCalls:  acc.Stats().ClassifierCalls,
		FocusUpdates:     mgr.Stats().FocusUpdates,
		Duration:         time.Since(startTime),
	}

	// Count proposals by state.
	for _, p := range wsStore.AllProposals() {
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

	for _, ws := range wsStore.ListWorkstreams("") {
		report.Workstreams = append(report.Workstreams, WorkstreamReport{
			ID:           ws.ID,
			Name:         ws.Name,
			Workspace:    ws.Workspace,
			State:        ws.State,
			Focus:        ws.Focus,
			SignalCount:  ws.SignalCount,
			Participants: ws.Participants,
			Created:      ws.Created,
			LastSignal:   ws.LastSignal,
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
