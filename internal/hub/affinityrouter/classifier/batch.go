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

// llmClassifyJSON is the JSON shape the classifier prompt asks the model to return.
type llmClassifyJSON struct {
	// Workstreams lists the IDs of existing workstreams these signals belong to.
	Workstreams []string `json:"workstreams"`

	NewWorkstreamName string `json:"new_workstream_name,omitempty"`

	NewWorkstreamFocus string `json:"new_workstream_focus,omitempty"`

	Confidence float64 `json:"confidence"`

	Reasoning string `json:"reasoning"`
}

// Result is the classification outcome for a batch of signals.
type Result struct {
	// WorkstreamIDs lists existing workstreams these signals belong to.
	// A signal can belong to multiple workstreams (multi-routing).
	WorkstreamIDs []string

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
// Returns which workstream(s) the signals belong to, or proposes a new one.
func (c *BatchClassifier) Classify(ctx context.Context, key models.ConversationKey, signals []models.Signal, active []models.Workstream, currentAffinityIDs []string) (*Result, error) {
	prompt := buildClassifyPrompt(key, signals, active, currentAffinityIDs)

	c.logger.Debug("classifying batch",
		"workspace", key.Workspace,
		"conversation", key.Conversation,
		"signals", len(signals),
		"active_workstreams", len(active),
	)

	var raw llmClassifyJSON
	if err := c.client.JSON(ctx, prompt, &raw); err != nil {
		return nil, fmt.Errorf("classify batch: %w", err)
	}

	result := &Result{
		Confidence: raw.Confidence,
		Reasoning:  raw.Reasoning,
	}

	if len(raw.Workstreams) > 0 {
		// Verify all returned workstream IDs exist.
		activeIDs := make(map[string]bool, len(active))
		for _, ws := range active {
			activeIDs[ws.ID] = true
		}
		for _, id := range raw.Workstreams {
			if activeIDs[id] {
				result.WorkstreamIDs = append(result.WorkstreamIDs, id)
			} else {
				c.logger.Warn("classifier returned unknown workstream ID", "id", id)
			}
		}
	}

	if raw.NewWorkstreamName != "" {
		result.NewWorkstreamName = raw.NewWorkstreamName
		result.NewWorkstreamFocus = raw.NewWorkstreamFocus
	}

	// If classifier returned nothing valid, route to default.
	if len(result.WorkstreamIDs) == 0 && result.NewWorkstreamName == "" {
		result.WorkstreamIDs = []string{models.DefaultWorkstreamID(key.Workspace)}
	}

	return result, nil
}

func buildClassifyPrompt(key models.ConversationKey, signals []models.Signal, active []models.Workstream, currentAffinityIDs []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are a workstream classifier for the %q workspace.\n\n", key.Workspace)
	fmt.Fprintf(&b, "Conversation: %s\n", key.Conversation)

	if len(currentAffinityIDs) > 0 {
		fmt.Fprintf(&b, "Current affinities: %s\n", strings.Join(currentAffinityIDs, ", "))
		b.WriteString("(This conversation has historically been affiliated with these workstreams. Only change this if the messages clearly indicate a different topic.)\n")
	}

	b.WriteString("\nActive workstreams:\n")
	for _, ws := range active {
		fmt.Fprintf(&b, "- ID: %s\n  Name: %s\n  Focus: %s\n\n",
			ws.ID, ws.Name, ws.Focus)
	}
	if len(active) == 0 {
		b.WriteString("(none — all signals are in the default stream)\n\n")
	}

	b.WriteString("Messages to classify:\n")
	for _, sig := range signals {
		fmt.Fprintf(&b, "[%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Sender, truncate(sig.Text, 300))
	}

	b.WriteString(`
Respond with a JSON object:
{
  "workstreams": ["<workstream_id_1>", "<workstream_id_2>"],
  "new_workstream_name": "",
  "new_workstream_focus": "",
  "confidence": 0.0,
  "reasoning": ""
}

Rules:
- MULTI-ROUTING: A signal can belong to MULTIPLE workstreams. If an incident, API change, deprecation notice, or status update affects multiple ongoing efforts, list ALL affected workstream IDs.
- If messages continue existing workstreams, list those IDs in "workstreams".
- Only propose a NEW workstream if the topic is genuinely novel AND substantive (an ongoing effort, not a one-off message). Set "new_workstream_name" and "new_workstream_focus". You can ALSO list existing workstream IDs alongside a new proposal if the signals touch both.
- Casual messages ("ok", "sounds good", lunch plans) should stay in the default stream — return an empty "workstreams" array.
- A workstream is a coherent ongoing effort — a project, a bug investigation, a deal, a feature. Not a single conversation topic.
- Respect existing affinities: if this conversation has been affiliated with a workstream for many signals, that affinity should persist unless there's clear evidence the topic has changed.

Respond with ONLY the JSON object, no other text.`)

	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func limitSlice(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
