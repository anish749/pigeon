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
	Workstreams        []string `json:"workstreams"`
	NewWorkstreamName  string   `json:"new_workstream_name,omitempty"`
	NewWorkstreamFocus string   `json:"new_workstream_focus,omitempty"`
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

	c.logger.Info("classifying batch",
		"account", key.Account.Display(),
		"conversation", key.Conversation,
		"signals", len(signals),
		"active_workstreams", len(active),
	)

	var raw llmClassifyJSON
	if err := c.client.JSON(ctx, prompt, &raw); err != nil {
		return nil, fmt.Errorf("classify batch: %w", err)
	}

	result := &Result{}

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
		result.WorkstreamIDs = []string{models.DefaultWorkstreamID(key.Account.String())}
	}

	return result, nil
}

func buildClassifyPrompt(key models.ConversationKey, signals []models.Signal, active []models.Workstream, currentAffinityIDs []string) string {
	var b strings.Builder

	// System context — explain what the system does and what we're asking.
	b.WriteString(`You are part of a messaging router that processes incoming signals (Slack messages, emails, calendar events, Linear issues, etc.) across a user's workspaces. The user works across multiple ongoing efforts simultaneously — projects, features, bug investigations, deals, partnerships, infrastructure work. We call each of these a "workstream."

Your job: given a batch of recent messages from a single conversation, determine which workstream(s) they relate to.

KEY CONCEPTS:
- A WORKSTREAM is a sustained, coherent effort that spans days or weeks. Examples: "ES 7.17 Upgrade", "Image Upload Feature", "Meta Partnership". It is NOT a single message topic or a casual exchange.
- A CONVERSATION is a Slack channel, DM, group DM, email thread, etc. One conversation often maps to one workstream (e.g. a DM with a colleague about a specific project), but channels can contain multiple workstreams.
- MULTI-ROUTING: One batch of messages can belong to multiple workstreams simultaneously. A deprecation notice, incident, or API change may affect several ongoing efforts. List ALL relevant workstream IDs when this happens.
- Messages like "ok", "sounds good", "call?", lunch plans, or general coordination that don't relate to any specific effort should NOT be assigned to a workstream — return an empty "workstreams" array so they stay in the general stream.

`)

	// Account and conversation context.
	fmt.Fprintf(&b, "ACCOUNT: %s\n", key.Account.Display())
	fmt.Fprintf(&b, "CONVERSATION: %s\n", key.Conversation)
	convType := "channel"
	if strings.HasPrefix(key.Conversation, "@") {
		if strings.Contains(key.Conversation, "mpdm") {
			convType = "group DM"
		} else {
			convType = "DM"
		}
	}
	fmt.Fprintf(&b, "CONVERSATION TYPE: %s\n", convType)

	if len(currentAffinityIDs) > 0 {
		fmt.Fprintf(&b, "CURRENT AFFINITY: This conversation has historically been routed to: %s\n", strings.Join(currentAffinityIDs, ", "))
		b.WriteString("This affinity was built over many messages. Only change it if the batch below clearly indicates a different topic.\n")
	}

	// Active workstreams with full context.
	b.WriteString("\nACTIVE WORKSTREAMS IN THIS WORKSPACE:\n")
	if len(active) == 0 {
		b.WriteString("(none yet — no workstreams have been created. If these messages represent a substantive ongoing effort, propose a new one.)\n")
	}
	for _, ws := range active {
		fmt.Fprintf(&b, "\n  ID: %s\n  Name: %s\n  Focus: %s\n  Active since: %s\n",
			ws.ID, ws.Name, ws.Focus, ws.Created.Format("2006-01-02"))
	}

	// The messages to classify.
	b.WriteString("\nMESSAGES TO CLASSIFY:\n")
	for _, sig := range signals {
		fmt.Fprintf(&b, "[%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Sender, sig.Text)
	}

	// Response format.
	b.WriteString(`
RESPOND with a JSON object:
{
  "workstreams": ["id1", "id2"],
  "new_workstream_name": "",
  "new_workstream_focus": ""
}

FIELD RULES:
- "workstreams": list the IDs of existing workstreams that these messages relate to. Can be multiple. Leave empty for general/casual messages or if proposing a new workstream that doesn't overlap with existing ones.
- "new_workstream_name": only set this if the messages represent a NEW ongoing effort not captured by any existing workstream. Use a short, descriptive name (e.g. "Auth Migration", "Q2 Pricing Page"). Do NOT propose a new workstream for one-off messages or casual chat.
- "new_workstream_focus": 1-2 sentence description of what the new workstream is about. Be specific — mention key technical terms, people, or systems so future messages can be matched to it.

You can set BOTH "workstreams" and "new_workstream_name" if the messages touch existing workstreams AND introduce a new effort.

Respond with ONLY the JSON object.`)

	return b.String()
}
