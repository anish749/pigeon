// Package manager implements workstream lifecycle management — updating focus
// descriptions, proposing merges/splits, and detecting dormancy/resolution.
package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anish749/pigeon/internal/hub/affinityrouter"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Manager periodically reviews workstreams and proposes lifecycle changes.
type Manager struct {
	store  *affinityrouter.Store
	client *clients.Client
	logger *slog.Logger

	// Stats
	focusUpdates int
}

// New creates a workstream manager.
func New(store *affinityrouter.Store, client *clients.Client, logger *slog.Logger) *Manager {
	return &Manager{
		store:  store,
		client: client,
		logger: logger,
	}
}

// UpdateFocus refreshes the focus description for a workstream based on
// its recent signals. Returns the updated focus, or an error.
func (m *Manager) UpdateFocus(ctx context.Context, ws *models.Workstream, recentSignals []models.Signal) (string, error) {
	if len(recentSignals) == 0 {
		return ws.Focus, nil
	}

	prompt := buildFocusPrompt(ws, recentSignals)
	newFocus, err := m.client.UpdateFocus(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("update focus for %s: %w", ws.ID, err)
	}

	ws.Focus = strings.TrimSpace(newFocus)
	m.store.UpdateWorkstream(ws)
	m.focusUpdates++

	m.logger.Info("focus updated",
		"workstream", ws.Name,
		"id", ws.ID,
		"signals_reviewed", len(recentSignals),
	)

	return ws.Focus, nil
}

func buildFocusPrompt(ws *models.Workstream, signals []models.Signal) string {
	var b strings.Builder

	b.WriteString("You are updating the focus description for a workstream. The focus is used by a classifier to route incoming messages to the right workstream, so it needs to be specific and current.\n\n")

	b.WriteString(fmt.Sprintf("Workstream: %s\n", ws.Name))
	b.WriteString(fmt.Sprintf("Workspace: %s\n", ws.Workspace))
	b.WriteString(fmt.Sprintf("Current focus: %s\n", ws.Focus))
	b.WriteString(fmt.Sprintf("Total signals: %d\n", ws.SignalCount))
	b.WriteString(fmt.Sprintf("Created: %s\n\n", ws.Created.Format("2006-01-02")))

	b.WriteString("Recent signals:\n")
	for _, sig := range signals {
		b.WriteString(fmt.Sprintf("[%s] [%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Conversation, sig.Sender, truncate(sig.Text, 200)))
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

// ManagerStats returns lifecycle management statistics.
type ManagerStats struct {
	FocusUpdates int
}

// Stats returns lifecycle management statistics.
func (m *Manager) Stats() ManagerStats {
	return ManagerStats{
		FocusUpdates: m.focusUpdates,
	}
}
