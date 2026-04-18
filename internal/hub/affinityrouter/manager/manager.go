// Package manager implements workstream lifecycle management. It owns all
// workstream state transitions: focus updates, dormancy detection, merge
// proposals, and proposal resolution. The accumulator routes signals; the
// manager manages what the signals are routed into.
package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Config controls manager behavior.
type Config struct {
	// FocusUpdateInterval — update a workstream's focus after this many new
	// signals have been routed to it since the last update.
	FocusUpdateInterval int

	// DormancyThreshold — mark a workstream dormant if no signals have
	// arrived for this duration.
	DormancyThreshold time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FocusUpdateInterval: 25,
		DormancyThreshold:   7 * 24 * time.Hour,
	}
}

// Manager owns the lifecycle of all workstreams. The only public entry
// point is ObserveSignal — the manager decides internally when to run
// focus updates, dormancy checks, and merge proposals.
type Manager struct {
	store  *affinityrouter.Store
	client *clients.Client
	cfg    Config
	logger *slog.Logger

	// Internal state.
	signalsSinceUpdate map[string]int    // workstreamID → signals since last focus update
	recentSignals      []models.Signal   // rolling buffer for focus update context

	// Stats
	focusUpdates    int
	dormancyChanges int
	mergesProposed  int
}

// New creates a workstream manager.
func New(store *affinityrouter.Store, client *clients.Client, cfg Config, logger *slog.Logger) *Manager {
	return &Manager{
		store:              store,
		client:             client,
		cfg:                cfg,
		logger:             logger,
		signalsSinceUpdate: make(map[string]int),
	}
}

// ObserveSignal is the single entry point. Call it for every routed signal.
// The manager tracks signal counts, buffers recent signals, and triggers
// lifecycle operations (focus updates, dormancy) when thresholds are met.
func (m *Manager) ObserveSignal(ctx context.Context, sig models.Signal) error {
	// Buffer the signal for focus update context.
	m.recentSignals = append(m.recentSignals, sig)
	if len(m.recentSignals) > m.cfg.FocusUpdateInterval*2 {
		// Keep the buffer bounded — retain the most recent signals.
		m.recentSignals = m.recentSignals[len(m.recentSignals)-m.cfg.FocusUpdateInterval:]
	}

	// Track per-workstream signal counts.
	for _, wsID := range sig.WorkstreamIDs {
		m.signalsSinceUpdate[wsID]++
	}

	// Check if any workstream needs a focus update.
	var errs []error
	for wsID, count := range m.signalsSinceUpdate {
		if count < m.cfg.FocusUpdateInterval {
			continue
		}

		ws := m.store.GetWorkstream(wsID)
		if ws == nil || ws.IsDefault() {
			m.signalsSinceUpdate[wsID] = 0
			continue
		}

		// Collect signals relevant to this workstream from the buffer.
		var relevant []models.Signal
		for _, s := range m.recentSignals {
			for _, id := range s.WorkstreamIDs {
				if id == wsID {
					relevant = append(relevant, s)
					break
				}
			}
		}

		if err := m.updateFocus(ctx, ws, relevant); err != nil {
			errs = append(errs, err)
		}
		m.signalsSinceUpdate[wsID] = 0
	}

	// Dormancy check (cheap — just timestamp comparisons).
	m.detectDormancy(sig.Ts)

	if len(errs) > 0 {
		return fmt.Errorf("manager: %w", errs[0])
	}
	return nil
}

// updateFocus refreshes the focus description for a single workstream.
func (m *Manager) updateFocus(ctx context.Context, ws *models.Workstream, recentSignals []models.Signal) error {
	if len(recentSignals) == 0 {
		return nil
	}

	prompt := buildFocusPrompt(ws, recentSignals)
	newFocus, err := m.client.Text(ctx, prompt)
	if err != nil {
		return fmt.Errorf("update focus for %s: %w", ws.ID, err)
	}

	ws.Focus = strings.TrimSpace(newFocus)
	m.store.UpdateWorkstream(ws)
	m.focusUpdates++

	m.logger.Info("focus updated",
		"workstream", ws.Name,
		"id", ws.ID,
		"signals_reviewed", len(recentSignals),
	)
	return nil
}

// detectDormancy marks workstreams as dormant if they haven't received
// signals within the dormancy threshold.
func (m *Manager) detectDormancy(now time.Time) {
	for _, ws := range m.store.ListWorkstreams("") {
		if ws.IsDefault() || ws.State != models.StateActive {
			continue
		}
		if !ws.LastSignal.IsZero() && now.Sub(ws.LastSignal) > m.cfg.DormancyThreshold {
			ws.State = models.StateDormant
			m.store.UpdateWorkstream(ws)
			m.dormancyChanges++
			m.logger.Info("workstream marked dormant",
				"workstream", ws.Name,
				"last_signal", ws.LastSignal.Format("2006-01-02"),
			)
		}
	}
}

func buildFocusPrompt(ws *models.Workstream, signals []models.Signal) string {
	var b strings.Builder

	b.WriteString("You are updating the focus description for a workstream. The focus is used by a classifier to route incoming messages to the right workstream, so it needs to be specific and current.\n\n")

	fmt.Fprintf(&b, "Workstream: %s\n", ws.Name)
	fmt.Fprintf(&b, "Workspace: %s\n", ws.Workspace)
	fmt.Fprintf(&b, "Current focus: %s\n", ws.Focus)
	fmt.Fprintf(&b, "Total signals: %d\n", ws.SignalCount)
	fmt.Fprintf(&b, "Created: %s\n\n", ws.Created.Format("2006-01-02"))

	b.WriteString("Recent signals:\n")
	for _, sig := range signals {
		fmt.Fprintf(&b, "[%s] [%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Conversation, sig.Sender, truncate(sig.Text, 200))
	}

	b.WriteString(`
Write an updated focus description (1-3 sentences) that:
1. Captures the current state/phase of this workstream
2. Mentions key technical terms, entities, or people that would help a classifier match new signals
3. Is specific enough to distinguish this workstream from others in the same workspace

Respond with ONLY the focus description text, nothing else.`)

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Stats returns lifecycle management statistics.
type Stats struct {
	FocusUpdates    int
	DormancyChanges int
	MergesProposed  int
}

// Stats returns lifecycle management statistics.
func (m *Manager) Stats() Stats {
	return Stats{
		FocusUpdates:    m.focusUpdates,
		DormancyChanges: m.dormancyChanges,
		MergesProposed:  m.mergesProposed,
	}
}
