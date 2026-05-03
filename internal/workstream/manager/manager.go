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

	"github.com/gosimple/slug"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/workstream/clients"
	"github.com/anish749/pigeon/internal/workstream/discovery"
	"github.com/anish749/pigeon/internal/workstream/models"
	wsstore "github.com/anish749/pigeon/internal/workstream/store"
)

// SignalReader reads historical signals for workspace accounts.
type SignalReader interface {
	ReadAccounts(ctx context.Context, accounts []account.Account, since, until time.Time) ([]models.Signal, error)
}

// Manager owns the lifecycle of all workstreams. It is the only component
// that creates or modifies workstreams. Its single entry point is
// ObserveRouting — call it after each routing decision is recorded.
type Manager struct {
	client *clients.Client
	disc   discovery.WorkspaceDiscovery
	reader SignalReader
	sc     *StatCollector
	store  wsstore.Store
	cfg    models.Config
	logger *slog.Logger

	// mu guards ephemeral in-process state below.
	mu sync.Mutex

	// Ephemeral tracking — not persisted.
	signalsSinceUpdate map[string]int  // workstreamID → signals since last focus update
	recentSignals      []models.Signal // rolling buffer for focus context

	// Stats
	focusUpdates int
}

// New creates a workstream manager.
func New(client *clients.Client, signalReader SignalReader, sc *StatCollector, cfg models.Config, st wsstore.Store, logger *slog.Logger) *Manager {
	return &Manager{
		client:             client,
		disc:               discovery.NewLLMDiscovery(client, logger),
		reader:             signalReader,
		sc:                 sc,
		store:              st,
		cfg:                cfg,
		logger:             logger,
		signalsSinceUpdate: make(map[string]int),
	}
}

// ReadSignals reads historical signals for the manager's configured workspace
// within the given time range. The returned signals are scoped to
// cfg.Workspace.Accounts and sorted by timestamp by the underlying reader.
func (m *Manager) ReadSignals(ctx context.Context, since, until time.Time) ([]models.Signal, error) {
	signals, err := m.reader.ReadAccounts(ctx, m.cfg.Workspace.Accounts, since, until)
	if err != nil {
		return nil, fmt.Errorf("read workspace signals: %w", err)
	}
	return signals, nil
}

// DiscoverAndPropose reads workspace signals in the given time range, runs
// LLM-based batch discovery, and routes each result through ProposeNew, so
// the same lifecycle invariants (idempotent existence check, proposal record,
// ID generation) apply. Under AutoApprove the proposals become workstreams
// immediately; otherwise they land in proposals.json for review.
//
// The now parameter is recorded as the Created timestamp on the default
// workstream (when freshly created) and on every discovered workstream.
// Callers pass the current wall-clock time from the outermost layer so
// Created reflects when discovery ran, not anything derived from the
// historical signal stream.
//
// Returns the raw LLM output so callers can display rich metadata
// (conversations, participants) that isn't kept on the persisted model.
// Existing same-named workstreams are left untouched, so re-runs preserve
// user edits to focus and state.
func (m *Manager) DiscoverAndPropose(ctx context.Context, since, until, now time.Time) ([]discovery.DiscoveredWorkstream, error) {
	signals, err := m.ReadSignals(ctx, since, until)
	if err != nil {
		return nil, err
	}
	return m.DiscoverAndProposeSignals(ctx, signals, now)
}

// DiscoverAndProposeSignals runs discovery over signals that were already read
// through ReadSignals. It exists for workflows, such as replay, that need the
// same signal set for additional processing and should not read the window
// twice.
func (m *Manager) DiscoverAndProposeSignals(ctx context.Context, signals []models.Signal, now time.Time) ([]discovery.DiscoveredWorkstream, error) {
	if len(signals) == 0 {
		return nil, nil
	}
	if err := m.EnsureDefaultWorkstream(m.cfg.Workspace.Name, now); err != nil {
		return nil, fmt.Errorf("ensure default workstream: %w", err)
	}
	discovered, err := m.disc.Discover(ctx, signals)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	wsName := m.cfg.Workspace.Name
	for _, d := range discovered {
		if _, err := m.ProposeNew(ctx, d.Name, d.Focus, wsName, now); err != nil {
			return nil, fmt.Errorf("propose %q: %w", d.Name, err)
		}
	}
	return discovered, nil
}

// EnsureDefaultWorkstream creates the default workstream for a workspace
// if it doesn't exist. The ts should be the timestamp of the first signal
// in this workspace.
func (m *Manager) EnsureDefaultWorkstream(ws config.WorkspaceName, ts time.Time) error {
	id := models.DefaultWorkstreamID(ws)
	_, ok, err := m.store.GetWorkstream(id)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return m.store.PutWorkstream(models.NewDefaultWorkstream(ws, ts))
}

// --- Lifecycle operations ---

// ProposeNew creates a workstream (AutoApprove) or queues a pending
// proposal for review (Interactive). In AutoApprove mode, returns the
// new workstream ID; in Interactive mode, returns the empty string and
// the proposal awaits ApproveProposal/RejectProposal.
func (m *Manager) ProposeNew(_ context.Context, name, focus string, ws config.WorkspaceName, proposedAt time.Time) (string, error) {
	if m.cfg.ApprovalMode == models.AutoApprove {
		w := models.Workstream{
			ID:        generateWorkstreamID(name),
			Name:      name,
			Workspace: ws,
			Focus:     focus,
			Created:   proposedAt,
		}

		_, exists, err := m.store.GetWorkstream(w.ID)
		if err != nil {
			return "", err
		}
		if exists {
			return w.ID, nil
		}
		if err := m.store.PutWorkstream(w); err != nil {
			return "", err
		}

		m.logger.Info("workstream created (auto-approved)",
			"workspace", string(ws),
			"name", name,
			"id", w.ID,
		)
		return w.ID, nil
	}

	// Queue for user confirmation.
	seq, err := m.store.NextProposalSeq()
	if err != nil {
		return "", err
	}
	proposal := &models.Proposal{
		ID:             fmt.Sprintf("p-%d", seq),
		SuggestedName:  name,
		SuggestedFocus: focus,
		Workspace:      ws,
		ProposedAt:     proposedAt,
	}
	if err := m.store.PutProposal(proposal); err != nil {
		return "", err
	}

	m.logger.Info("workstream proposed (pending confirmation)",
		"workspace", string(ws),
		"name", name,
		"proposal", proposal.ID,
	)
	return "", nil
}

// ApproveProposal creates the workstream for a pending proposal and
// removes the proposal from the queue. Returns the resulting workstream
// ID. Errors if the proposal is missing or would collide with an
// existing workstream (same slug ID).
//
// On collision the proposal is left in place and nothing is written —
// the user must reject the proposal or rename one side. Approval must
// never overwrite a workstream that may carry user edits.
//
// The workstream is written before the proposal is deleted. If the
// delete fails after a successful write, the workstream is the source
// of truth; a retry of ApproveProposal will surface the orphaned
// proposal as a slug conflict, which the user can clear with
// RejectProposal.
func (m *Manager) ApproveProposal(_ context.Context, id string) (string, error) {
	p, ok, err := m.store.GetProposal(id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("proposal %q not found", id)
	}

	w := models.Workstream{
		ID:        generateWorkstreamID(p.SuggestedName),
		Name:      p.SuggestedName,
		Workspace: p.Workspace,
		Focus:     p.SuggestedFocus,
		Created:   p.ProposedAt,
	}
	_, exists, err := m.store.GetWorkstream(w.ID)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("proposal %q would conflict with existing workstream %q — reject this proposal or rename one side", id, w.ID)
	}
	if err := m.store.PutWorkstream(w); err != nil {
		return "", err
	}

	if err := m.store.DeleteProposal(id); err != nil {
		return "", fmt.Errorf("delete proposal after creating workstream %q: %w", w.ID, err)
	}
	m.logger.Info("proposal approved", "id", id, "workstream", w.ID)
	return w.ID, nil
}

// RejectProposal removes a pending proposal from the queue without
// applying it. Errors if the proposal is missing.
func (m *Manager) RejectProposal(_ context.Context, id string) error {
	_, ok, err := m.store.GetProposal(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("proposal %q not found", id)
	}
	if err := m.store.DeleteProposal(id); err != nil {
		return err
	}
	m.logger.Info("proposal rejected", "id", id)
	return nil
}

// ObserveRouting records the routing decision in the stat collector and triggers
// lifecycle operations (focus updates, dormancy). This is the single entry
// point — the manager owns the stat collector, so recording happens here.
func (m *Manager) ObserveRouting(ctx context.Context, sig models.Signal, decision models.RoutingDecision) error {
	// Record in the stat collector (has its own lock).
	m.sc.Record(decision, sig)

	m.mu.Lock()

	// Buffer the signal for focus update context.
	m.recentSignals = append(m.recentSignals, sig)
	if len(m.recentSignals) > m.cfg.FocusUpdateInterval*2 {
		m.recentSignals = m.recentSignals[len(m.recentSignals)-m.cfg.FocusUpdateInterval:]
	}

	// Track per-workstream signal counts.
	for _, wsID := range decision.WorkstreamIDs {
		m.signalsSinceUpdate[wsID]++
	}

	// Collect workstreams that need a focus update.
	var needsUpdate []string
	for wsID, count := range m.signalsSinceUpdate {
		if count >= m.cfg.FocusUpdateInterval {
			needsUpdate = append(needsUpdate, wsID)
		}
	}
	recentSignals := make([]models.Signal, len(m.recentSignals))
	copy(recentSignals, m.recentSignals)

	m.mu.Unlock()

	// Focus updates (store + LLM calls) happen outside the lock.
	var errs []error
	for _, wsID := range needsUpdate {
		ws, ok, err := m.store.GetWorkstream(wsID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok || ws.IsDefault() {
			m.mu.Lock()
			m.signalsSinceUpdate[wsID] = 0
			m.mu.Unlock()
			continue
		}

		if err := m.updateFocus(ctx, ws, recentSignals); err != nil {
			errs = append(errs, err)
		}
		m.mu.Lock()
		m.signalsSinceUpdate[wsID] = 0
		m.mu.Unlock()
	}

	if len(errs) > 0 {
		return fmt.Errorf("manager: %w", errs[0])
	}
	return nil
}

// --- Internal lifecycle operations ---

func (m *Manager) updateFocus(ctx context.Context, ws models.Workstream, recentSignals []models.Signal) error {
	if len(recentSignals) == 0 {
		return nil
	}

	prompt := buildFocusPrompt(ws, recentSignals)
	newFocus, err := m.client.Text(ctx, "You are a concise workstream summarizer. Respond only with the requested description.", prompt)
	if err != nil {
		return fmt.Errorf("update focus for %s: %w", ws.ID, err)
	}

	updated := ws.WithFocus(strings.TrimSpace(newFocus))
	if err := m.store.PutWorkstream(updated); err != nil {
		return err
	}
	m.mu.Lock()
	m.focusUpdates++
	m.mu.Unlock()

	m.logger.Info("focus updated",
		"workstream", ws.Name,
		"id", ws.ID,
		"signals_reviewed", len(recentSignals),
	)
	return nil
}

func buildFocusPrompt(ws models.Workstream, signals []models.Signal) string {
	var b strings.Builder

	b.WriteString(`You are writing the "focus description" for a workstream.

WHAT A FOCUS DESCRIPTION IS:
A focus description is a short paragraph that a classifier reads to decide whether an incoming message belongs to this workstream. When a new Slack message, email, or notification arrives, the classifier compares it against the focus descriptions of all active workstreams to determine where it should be routed.

WHAT MAKES A GOOD FOCUS DESCRIPTION:
- Specific enough that RELATED messages match: mention the key technical terms, system names, people, tickets, repos, and concepts that appear in this workstream's messages.
- Distinct enough that UNRELATED messages don't match: avoid generic terms that could apply to any workstream.
- Current: reflect what the workstream is about RIGHT NOW, not what it was about a month ago. If the work has shifted from planning to debugging to deployment, say so.

WHAT TO AVOID:
- Generic descriptions like "engineering work" or "product discussion"
- Listing every detail — focus on the distinguishing characteristics
- Past tense summaries — describe the current state

`)

	fmt.Fprintf(&b, "WORKSTREAM: %s\n", ws.Name)
	fmt.Fprintf(&b, "WORKSPACE: %s\n", string(ws.Workspace))
	fmt.Fprintf(&b, "CURRENT FOCUS (may be outdated): %s\n\n", ws.Focus)

	b.WriteString("RECENT MESSAGES (use these to understand what the workstream is about now):\n")
	for _, sig := range signals {
		fmt.Fprintf(&b, "[%s] [%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Conversation, sig.Sender, sig.Text)
	}

	b.WriteString("\nWrite an updated focus description (1-3 sentences). Respond with ONLY the description, nothing else.")

	return b.String()
}

func generateWorkstreamID(name string) string {
	return "ws-" + slug.Make(name)
}

// Stats returns lifecycle management statistics.
type Stats struct {
	FocusUpdates int
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		FocusUpdates: m.focusUpdates,
	}
}
