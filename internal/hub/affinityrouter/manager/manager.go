// Package manager implements workstream lifecycle management. It is the ONLY
// component that creates, updates, or transitions workstreams. The router
// routes signals to existing workstreams; the manager decides what workstreams
// exist.
package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Ledger is the interface the manager needs to query routing stats.
// Satisfied by ledger.Ledger.
type Ledger interface {
	SignalCount(workstreamID string) int
	Participants(workstreamID string) []string
	LastSignal(workstreamID string) time.Time
}

// Config controls manager behavior.
type Config struct {
	// FocusUpdateInterval — update a workstream's focus after this many new
	// signals have been routed to it since the last update.
	FocusUpdateInterval int

	// DormancyThreshold — mark a workstream dormant if no signals have
	// arrived for this duration.
	DormancyThreshold time.Duration

	// ApprovalMode controls how proposals are handled.
	ApprovalMode models.ApprovalMode
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FocusUpdateInterval: 25,
		DormancyThreshold:   7 * 24 * time.Hour,
		ApprovalMode:        models.AutoApprove,
	}
}

// Manager owns the lifecycle of all workstreams. It is the only component
// that creates or modifies workstreams. Its single entry point is
// ObserveRouting — call it after each routing decision is recorded.
type Manager struct {
	mu     sync.RWMutex
	client *clients.Client
	ledger Ledger
	cfg    Config
	logger *slog.Logger

	// Workstream storage — manager owns this.
	workstreams map[string]models.Workstream

	// Proposal queue.
	proposals   []*models.Proposal
	proposalSeq int

	// Internal tracking.
	signalsSinceUpdate map[string]int  // workstreamID → signals since last focus update
	recentSignals      []models.Signal // rolling buffer for focus context

	// Stats
	focusUpdates    int
	dormancyChanges int
}

// New creates a workstream manager.
func New(client *clients.Client, ledger Ledger, cfg Config, logger *slog.Logger) *Manager {
	return &Manager{
		client:             client,
		ledger:             ledger,
		cfg:                cfg,
		logger:             logger,
		workstreams:        make(map[string]models.Workstream),
		signalsSinceUpdate: make(map[string]int),
	}
}

// --- Workstream queries (read-only) ---

// GetWorkstream returns a workstream by ID, or zero value + false if not found.
func (m *Manager) GetWorkstream(id string) (models.Workstream, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws, ok := m.workstreams[id]
	return ws, ok
}

// ActiveWorkstreams returns non-default, active workstreams for a workspace.
func (m *Manager) ActiveWorkstreams(workspace string) []models.Workstream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []models.Workstream
	for _, ws := range m.workstreams {
		if ws.Workspace == workspace && ws.State == models.StateActive && !ws.IsDefault() {
			result = append(result, ws)
		}
	}
	return result
}

// AllWorkstreams returns all workstreams (for reporting).
func (m *Manager) AllWorkstreams() []models.Workstream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]models.Workstream, 0, len(m.workstreams))
	for _, ws := range m.workstreams {
		result = append(result, ws)
	}
	return result
}

// EnsureDefaultWorkstream creates the default workstream for a workspace
// if it doesn't exist.
func (m *Manager) EnsureDefaultWorkstream(workspace string) {
	id := models.DefaultWorkstreamID(workspace)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workstreams[id]; !ok {
		m.workstreams[id] = models.NewDefaultWorkstream(workspace)
	}
}

// --- Lifecycle operations ---

// ProposeNew queues a proposal to create a new workstream. In AutoApprove
// mode, creates immediately. Returns the new workstream ID if created.
func (m *Manager) ProposeNew(ctx context.Context, name, focus, workspace string, triggerSignals []models.Signal, confidence float64, reasoning string) (string, error) {
	proposal := &models.Proposal{
		Type:           models.ProposalCreate,
		SuggestedName:  name,
		SuggestedFocus: focus,
		Workspace:      workspace,
		Confidence:     confidence,
		Reasoning:      reasoning,
		ProposedAt:     time.Now(),
	}

	if m.cfg.ApprovalMode == models.AutoApprove {
		ws := models.Workstream{
			ID:        generateWorkstreamID(name),
			Name:      name,
			Workspace: workspace,
			State:     models.StateActive,
			Focus:     focus,
			Created:   triggerSignals[0].Ts,
		}

		m.mu.Lock()
		if _, exists := m.workstreams[ws.ID]; exists {
			m.mu.Unlock()
			return ws.ID, nil // already exists, reuse
		}
		m.workstreams[ws.ID] = ws
		proposal.State = models.ProposalApproved
		m.proposals = append(m.proposals, proposal)
		m.mu.Unlock()

		m.logger.Info("workstream created (auto-approved)",
			"workspace", workspace,
			"name", name,
			"id", ws.ID,
		)
		return ws.ID, nil
	}

	// Queue for user confirmation.
	m.mu.Lock()
	m.proposalSeq++
	proposal.ID = fmt.Sprintf("p-%d", m.proposalSeq)
	proposal.State = models.ProposalPending
	m.proposals = append(m.proposals, proposal)
	m.mu.Unlock()

	m.logger.Info("workstream proposed (pending confirmation)",
		"workspace", workspace,
		"name", name,
		"confidence", confidence,
	)
	return "", nil
}

// ObserveRouting is called after each routing decision is recorded in the
// ledger. The manager uses it to track focus staleness and trigger lifecycle
// operations. This is the single entry point.
func (m *Manager) ObserveRouting(ctx context.Context, sig models.Signal, decision models.RoutingDecision) error {
	// Buffer the signal for focus update context.
	m.recentSignals = append(m.recentSignals, sig)
	if len(m.recentSignals) > m.cfg.FocusUpdateInterval*2 {
		m.recentSignals = m.recentSignals[len(m.recentSignals)-m.cfg.FocusUpdateInterval:]
	}

	// Track per-workstream signal counts.
	for _, wsID := range decision.WorkstreamIDs {
		m.signalsSinceUpdate[wsID]++
	}

	// Check if any workstream needs a focus update.
	var errs []error
	for wsID, count := range m.signalsSinceUpdate {
		if count < m.cfg.FocusUpdateInterval {
			continue
		}
		ws, ok := m.GetWorkstream(wsID)
		if !ok || ws.IsDefault() {
			m.signalsSinceUpdate[wsID] = 0
			continue
		}

		var relevant []models.Signal
		for _, s := range m.recentSignals {
			relevant = append(relevant, s)
		}

		if err := m.updateFocus(ctx, ws, relevant); err != nil {
			errs = append(errs, err)
		}
		m.signalsSinceUpdate[wsID] = 0
	}

	// Dormancy check.
	m.detectDormancy(sig.Ts)

	if len(errs) > 0 {
		return fmt.Errorf("manager: %w", errs[0])
	}
	return nil
}

// --- Proposals ---

// PendingProposals returns unresolved proposals.
func (m *Manager) PendingProposals() []*models.Proposal {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var pending []*models.Proposal
	for _, p := range m.proposals {
		if p.State == models.ProposalPending {
			pending = append(pending, p)
		}
	}
	return pending
}

// AllProposals returns all proposals (for reporting).
func (m *Manager) AllProposals() []*models.Proposal {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*models.Proposal, len(m.proposals))
	copy(result, m.proposals)
	return result
}

// --- Internal lifecycle operations ---

func (m *Manager) updateFocus(ctx context.Context, ws models.Workstream, recentSignals []models.Signal) error {
	if len(recentSignals) == 0 {
		return nil
	}

	prompt := buildFocusPrompt(ws, recentSignals, m.ledger)
	newFocus, err := m.client.Text(ctx, prompt)
	if err != nil {
		return fmt.Errorf("update focus for %s: %w", ws.ID, err)
	}

	updated := ws.WithFocus(strings.TrimSpace(newFocus))
	m.mu.Lock()
	m.workstreams[ws.ID] = updated
	m.mu.Unlock()
	m.focusUpdates++

	m.logger.Info("focus updated",
		"workstream", ws.Name,
		"id", ws.ID,
		"signals_reviewed", len(recentSignals),
	)
	return nil
}

func (m *Manager) detectDormancy(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ws := range m.workstreams {
		if ws.IsDefault() || ws.State != models.StateActive {
			continue
		}
		lastSig := m.ledger.LastSignal(id)
		if !lastSig.IsZero() && now.Sub(lastSig) > m.cfg.DormancyThreshold {
			m.workstreams[id] = ws.WithState(models.StateDormant)
			m.dormancyChanges++
			m.logger.Info("workstream marked dormant",
				"workstream", ws.Name,
				"last_signal", lastSig.Format("2006-01-02"),
			)
		}
	}
}

func buildFocusPrompt(ws models.Workstream, signals []models.Signal, l Ledger) string {
	var b strings.Builder

	b.WriteString("You are updating the focus description for a workstream. The focus is used by a classifier to route incoming messages to the right workstream, so it needs to be specific and current.\n\n")

	fmt.Fprintf(&b, "Workstream: %s\n", ws.Name)
	fmt.Fprintf(&b, "Workspace: %s\n", ws.Workspace)
	fmt.Fprintf(&b, "Current focus: %s\n", ws.Focus)
	fmt.Fprintf(&b, "Total signals: %d\n", l.SignalCount(ws.ID))
	fmt.Fprintf(&b, "Participants: %s\n", strings.Join(l.Participants(ws.ID), ", "))
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

func generateWorkstreamID(name string) string {
	var b strings.Builder
	b.WriteString("ws-")
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else if c >= 'A' && c <= 'Z' {
			b.WriteRune(c + 32)
		} else if c == ' ' || c == '-' || c == '_' {
			b.WriteByte('-')
		}
	}
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
	WorkstreamCount int
	ProposalCount   int
}

func (m *Manager) Stats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Stats{
		FocusUpdates:    m.focusUpdates,
		DormancyChanges: m.dormancyChanges,
		WorkstreamCount: len(m.workstreams),
		ProposalCount:   len(m.proposals),
	}
}
