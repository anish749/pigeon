// Package classifier implements batch classification of signals against
// active workstreams using the Claude CLI client.
package classifier

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// Result is the classification outcome for a batch of signals.
type Result struct {
	// WorkstreamID is the ID of the existing workstream these signals belong to.
	// Empty if proposing a new workstream.
	WorkstreamID string

	// NewWorkstreamName is set when proposing a new workstream.
	NewWorkstreamName string

	// NewWorkstreamFocus is the proposed focus description for a new workstream.
	NewWorkstreamFocus string

	// Confidence is the classifier's confidence in the routing decision (0-1).
	Confidence float64

	// Reasoning explains the classification decision.
	Reasoning string
}

// BatchClassifier uses the Claude CLI to classify batches of signals
// against active workstreams.
type BatchClassifier struct {
	client *clients.Client
	logger *slog.Logger
}

// New creates a batch classifier.
func New(client *clients.Client, logger *slog.Logger) *BatchClassifier {
	return &BatchClassifier{
		client: client,
		logger: logger,
	}
}

// Classify classifies a batch of signals against active workstreams.
// Returns which workstream the signals belong to, or proposes a new one.
func (c *BatchClassifier) Classify(ctx context.Context, key models.ConversationKey, signals []models.Signal, active []*models.Workstream) (*Result, error) {
	prompt := buildClassifyPrompt(key, signals, active)

	c.logger.Debug("classifying batch",
		"workspace", key.Workspace,
		"conversation", key.Conversation,
		"signals", len(signals),
		"active_workstreams", len(active),
	)

	resp, err := c.client.Classify(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("classify batch: %w", err)
	}

	result := &Result{
		Confidence: resp.Confidence,
		Reasoning:  resp.Reasoning,
	}

	if resp.Workstream != "" {
		// Verify the workstream ID exists in the active list.
		found := false
		for _, ws := range active {
			if ws.ID == resp.Workstream {
				found = true
				break
			}
		}
		if found {
			result.WorkstreamID = resp.Workstream
		} else {
			// Classifier returned an unknown ID — treat as default.
			c.logger.Warn("classifier returned unknown workstream ID",
				"id", resp.Workstream,
				"workspace", key.Workspace,
			)
			result.WorkstreamID = models.DefaultWorkstreamID(key.Workspace)
		}
	} else if resp.NewWorkstreamName != "" {
		result.NewWorkstreamName = resp.NewWorkstreamName
		result.NewWorkstreamFocus = resp.NewWorkstreamFocus
	} else {
		// No workstream and no proposal — route to default.
		result.WorkstreamID = models.DefaultWorkstreamID(key.Workspace)
	}

	return result, nil
}

func buildClassifyPrompt(key models.ConversationKey, signals []models.Signal, active []*models.Workstream) string {
	var b strings.Builder

	b.WriteString("You are a workstream classifier. Given a batch of messages from a conversation and a list of active workstreams, determine which workstream these messages belong to.\n\n")

	b.WriteString(fmt.Sprintf("Workspace: %s\n", key.Workspace))
	b.WriteString(fmt.Sprintf("Conversation: %s\n\n", key.Conversation))

	b.WriteString("Active workstreams:\n")
	for _, ws := range active {
		b.WriteString(fmt.Sprintf("- ID: %s\n  Name: %s\n  Focus: %s\n  Signals so far: %d\n\n", ws.ID, ws.Name, ws.Focus, ws.SignalCount))
	}
	if len(active) == 0 {
		b.WriteString("(none — all signals are currently in the default/general stream)\n\n")
	}

	b.WriteString("Messages to classify:\n")
	for _, sig := range signals {
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Sender, truncate(sig.Text, 300)))
	}

	b.WriteString(`
Respond with a JSON object:
{
  "workstream": "<workstream ID if these messages belong to an existing workstream, empty string if proposing new>",
  "new_workstream_name": "<short name for the new workstream, only if workstream is empty>",
  "new_workstream_focus": "<1-3 sentence description of what this workstream is about, only if workstream is empty>",
  "confidence": <0.0 to 1.0>,
  "reasoning": "<brief explanation of your decision>"
}

Rules:
- If the messages clearly continue an existing workstream, use its ID.
- If the messages are about a topic that doesn't match any existing workstream AND the topic seems substantive enough to be its own workstream (not just casual chat), propose a new workstream.
- If the messages are casual/generic ("ok", "sounds good", lunch plans), use the default workstream for this workspace (don't propose a new workstream).
- A workstream should represent a coherent ongoing effort — a project, a bug investigation, a deal, a feature build. Not a single message topic.
- The conversation name gives strong signal: DMs with a specific person often map to a consistent workstream. Channel names hint at the domain.
- When in doubt, keep signals in the default stream. It's better to miss a workstream than to create false ones.

Respond with ONLY the JSON object, no other text.`)

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
