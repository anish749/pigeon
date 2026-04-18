package classifier

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

const defaultWindowSize = 25

// llmClassifyJSON is the JSON shape the classifier prompt asks the model to return.
type llmClassifyJSON struct {
	Workstreams        []string `json:"workstreams"`
	NewWorkstreamName  string   `json:"new_workstream_name,omitempty"`
	NewWorkstreamFocus string   `json:"new_workstream_focus,omitempty"`
}

// BatchClassifier buffers signals in a sliding window and classifies them
// in batch against active workstreams using the Claude CLI. Each instance
// is scoped to a single conversation.
type BatchClassifier struct {
	client     *clients.Client
	logger     *slog.Logger
	windowSize int

	// Sliding window of recent signals.
	window []models.Signal

	// Per-signal routing decisions — tracks what was last decided for each
	// signal (by the router or by a prior classification).
	decisions map[string][]string // signal ID → workstream IDs
}

// Observe buffers a signal and records the router's routing decision.
func (c *BatchClassifier) Observe(sig models.Signal, decision models.RoutingDecision) {
	c.appendToWindow(sig)
	c.decisions[sig.ID] = decision.WorkstreamIDs
}

// ObserveAndClassify buffers the signal, then runs LLM classification on
// the full window. Returns signals whose assignment changed.
func (c *BatchClassifier) ObserveAndClassify(ctx context.Context, sig models.Signal, acct account.Account, conversation string, workstreams []models.Workstream, affinityIDs []string) (*Result, error) {
	c.appendToWindow(sig)

	prompt := buildClassifyPrompt(acct, conversation, c.window, workstreams, affinityIDs)

	c.logger.Info("classifying batch",
		"account", acct.Display(),
		"conversation", conversation,
		"signals", len(c.window),
		"active_workstreams", len(workstreams),
	)

	var raw llmClassifyJSON
	if err := c.client.JSON(ctx, "You are a concise workstream classifier. Respond only with the requested JSON.", prompt, &raw); err != nil {
		return nil, fmt.Errorf("classify batch: %w", err)
	}

	// Validate returned workstream IDs.
	var newIDs []string
	if len(raw.Workstreams) > 0 {
		activeIDs := make(map[string]bool, len(workstreams))
		for _, ws := range workstreams {
			activeIDs[ws.ID] = true
		}
		for _, id := range raw.Workstreams {
			if activeIDs[id] {
				newIDs = append(newIDs, id)
			} else {
				c.logger.Warn("classifier returned unknown workstream ID", "id", id)
			}
		}
	}

	// Diff: find signals whose assignment changed.
	result := &Result{}
	for _, s := range c.window {
		prev := c.decisions[s.ID]
		if !equalStringSlices(prev, newIDs) {
			result.Routings = append(result.Routings, SignalRouting{
				Signal:        s,
				WorkstreamIDs: newIDs,
			})
		}
		// Update tracking to reflect the classification.
		c.decisions[s.ID] = newIDs
	}

	if raw.NewWorkstreamName != "" {
		result.NewWorkstreamName = raw.NewWorkstreamName
		result.NewWorkstreamFocus = raw.NewWorkstreamFocus
	}

	return result, nil
}

// Buffered returns the number of signals in the window.
func (c *BatchClassifier) Buffered() int {
	return len(c.window)
}

func (c *BatchClassifier) appendToWindow(sig models.Signal) {
	c.window = append(c.window, sig)
	if len(c.window) > c.windowSize {
		// Evict oldest signals and clean up their decision tracking.
		evicted := c.window[:len(c.window)-c.windowSize]
		for _, s := range evicted {
			delete(c.decisions, s.ID)
		}
		c.window = c.window[len(c.window)-c.windowSize:]
	}
}

// equalStringSlices reports whether two string slices contain the same
// elements regardless of order.
func equalStringSlices(a, b []string) bool {
	sa := slices.Clone(a)
	sb := slices.Clone(b)
	slices.Sort(sa)
	slices.Sort(sb)
	return slices.Equal(sa, sb)
}

// NewBatchFactory returns a Factory that creates BatchClassifiers sharing
// the given client and logger.
func NewBatchFactory(client *clients.Client, logger *slog.Logger) Factory {
	return func() WorkstreamClassifier {
		return &BatchClassifier{
			client:     client,
			logger:     logger,
			windowSize: defaultWindowSize,
			decisions:  make(map[string][]string),
		}
	}
}

func buildClassifyPrompt(acct account.Account, conversation string, signals []models.Signal, active []models.Workstream, currentAffinityIDs []string) string {
	var b strings.Builder

	b.WriteString(`You are part of a messaging router that processes incoming signals (Slack messages, emails, calendar events, Linear issues, etc.) across a user's workspaces. The user works across multiple ongoing efforts simultaneously — projects, features, bug investigations, deals, partnerships, infrastructure work. We call each of these a "workstream."

Your job: given a batch of recent messages from a single conversation, determine which workstream(s) they relate to.

KEY CONCEPTS:
- A WORKSTREAM is a sustained, coherent effort that spans days or weeks. Examples: "ES 7.17 Upgrade", "Image Upload Feature", "Meta Partnership". It is NOT a single message topic or a casual exchange.
- A CONVERSATION is a Slack channel, DM, group DM, email thread, etc. One conversation often maps to one workstream (e.g. a DM with a colleague about a specific project), but channels can contain multiple workstreams.
- MULTI-ROUTING: One batch of messages can belong to multiple workstreams simultaneously. A deprecation notice, incident, or API change may affect several ongoing efforts. List ALL relevant workstream IDs when this happens.
- Messages like "ok", "sounds good", "call?", lunch plans, or general coordination that don't relate to any specific effort should NOT be assigned to a workstream — return an empty "workstreams" array so they stay in the general stream.

`)

	fmt.Fprintf(&b, "ACCOUNT: %s\n", acct.Display())
	fmt.Fprintf(&b, "CONVERSATION: %s\n", conversation)
	convType := "channel"
	if strings.HasPrefix(conversation, "@") {
		if strings.Contains(conversation, "mpdm") {
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

	b.WriteString("\nACTIVE WORKSTREAMS IN THIS WORKSPACE:\n")
	if len(active) == 0 {
		b.WriteString("(none yet — no workstreams have been created. If these messages represent a substantive ongoing effort, propose a new one.)\n")
	}
	for _, ws := range active {
		fmt.Fprintf(&b, "\n  ID: %s\n  Name: %s\n  Focus: %s\n  Active since: %s\n",
			ws.ID, ws.Name, ws.Focus, ws.Created.Format("2006-01-02"))
	}

	b.WriteString("\nMESSAGES TO CLASSIFY:\n")
	for _, sig := range signals {
		fmt.Fprintf(&b, "[%s] %s: %s\n", sig.Ts.Format("2006-01-02 15:04"), sig.Sender, sig.Text)
	}

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
