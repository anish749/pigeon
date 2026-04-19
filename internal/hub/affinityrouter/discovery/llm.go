package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anish749/pigeon/internal/hub/affinityrouter/clients"
	"github.com/anish749/pigeon/internal/hub/affinityrouter/models"
)

// llmDiscoverJSON is the JSON shape returned by the discovery prompt.
type llmDiscoverJSON struct {
	Workstreams []llmWorkstream `json:"workstreams"`
}

type llmWorkstream struct {
	Name          string   `json:"name"`
	Focus         string   `json:"focus"`
	Conversations []string `json:"conversations"`
	Participants  []string `json:"participants"`
}

// LLMDiscovery uses the Claude CLI to discover workstreams from a batch of signals.
// It works in two steps:
//  1. Summarize each conversation into a compact digest (conversation name, participants,
//     topic summary, key messages). This reduces 500+ signals to ~20 conversation summaries.
//  2. Send all conversation summaries to the LLM in a single call and ask it to identify
//     distinct workstreams that span conversations.
type LLMDiscovery struct {
	client    *clients.Client
	logger    *slog.Logger
	longModel string // model for discovery (can use a more capable model than routing)
}

// NewLLMDiscovery creates a discovery instance backed by the Claude CLI.
// longModel is optional — if empty, uses the same client model.
func NewLLMDiscovery(client *clients.Client, logger *slog.Logger) *LLMDiscovery {
	return &LLMDiscovery{client: client, logger: logger}
}

// WithModel sets a specific model for discovery (e.g. "sonnet" for better reasoning on large prompts).
func (d *LLMDiscovery) WithModel(model string) *LLMDiscovery {
	d.longModel = model
	return d
}

// conversationDigest is a compact summary of one conversation's signals.
type conversationDigest struct {
	name         string
	participants []string
	signalCount  int
	dateRange    string
	messages     []string // representative messages (first, middle, last)
}

// Discover analyzes all signals and discovers workstreams.
func (d *LLMDiscovery) Discover(ctx context.Context, signals []models.Signal) ([]DiscoveredWorkstream, error) {
	if len(signals) == 0 {
		return nil, fmt.Errorf("no signals to discover workstreams from")
	}

	// Step 1: Build per-conversation digests.
	digests := d.buildDigests(signals)
	d.logger.Info("discovery: built conversation digests", "conversations", len(digests), "total_signals", len(signals))

	// Step 2: Send digests to LLM for workstream discovery.
	prompt := buildDiscoveryPrompt(digests)
	d.logger.Info("discovery: calling LLM", "prompt_length", len(prompt))

	var result llmDiscoverJSON
	if err := d.client.JSON(ctx, discoverySystemPrompt, prompt, &result); err != nil {
		return nil, fmt.Errorf("discovery LLM call: %w", err)
	}

	d.logger.Info("discovery: LLM returned", "workstreams", len(result.Workstreams))

	// Convert to output type.
	var discovered []DiscoveredWorkstream
	for _, ws := range result.Workstreams {
		d.logger.Info("discovery: found workstream", "name", ws.Name, "focus", ws.Focus, "conversations", ws.Conversations, "participants", ws.Participants)
		discovered = append(discovered, DiscoveredWorkstream{
			Name:          ws.Name,
			Focus:         ws.Focus,
			Conversations: ws.Conversations,
			Participants:  ws.Participants,
		})
	}

	return discovered, nil
}

// buildDigests groups signals by conversation and creates compact summaries.
func (d *LLMDiscovery) buildDigests(signals []models.Signal) []conversationDigest {
	// Group by conversation.
	type convData struct {
		signals      []models.Signal
		participants map[string]bool
	}
	convs := make(map[string]*convData)
	var convOrder []string

	for _, sig := range signals {
		cd, ok := convs[sig.Conversation]
		if !ok {
			cd = &convData{participants: make(map[string]bool)}
			convs[sig.Conversation] = cd
			convOrder = append(convOrder, sig.Conversation)
		}
		cd.signals = append(cd.signals, sig)
		if sig.Sender != "" {
			cd.participants[sig.Sender] = true
		}
	}

	var digests []conversationDigest
	for _, name := range convOrder {
		cd := convs[name]
		if len(cd.signals) < 2 {
			continue // skip conversations with < 2 messages
		}

		// Collect participants.
		var participants []string
		for p := range cd.participants {
			participants = append(participants, p)
		}

		// Build date range.
		first := cd.signals[0].Ts
		last := cd.signals[len(cd.signals)-1].Ts
		dateRange := first.Format("Jan 02") + " – " + last.Format("Jan 02")

		// Select representative messages: up to 15 spread across the conversation.
		messages := selectRepresentative(cd.signals, 15)

		digests = append(digests, conversationDigest{
			name:         name,
			participants: participants,
			signalCount:  len(cd.signals),
			dateRange:    dateRange,
			messages:     messages,
		})
	}

	return digests
}

// selectRepresentative picks up to n representative messages from a conversation,
// evenly spread across the timeline. Skips very short messages (< 15 chars).
func selectRepresentative(signals []models.Signal, n int) []string {
	// Filter to substantive messages.
	var substantive []models.Signal
	for _, s := range signals {
		if len(s.Text) >= 15 {
			substantive = append(substantive, s)
		}
	}
	if len(substantive) == 0 {
		// Fall back to all messages if none are substantive.
		substantive = signals
	}

	// Take evenly spaced samples.
	if len(substantive) <= n {
		var msgs []string
		for _, s := range substantive {
			msgs = append(msgs, fmt.Sprintf("[%s] %s: %s", s.Ts.Format("Jan 02 15:04"), s.Sender, s.Text))
		}
		return msgs
	}

	step := float64(len(substantive)) / float64(n)
	var msgs []string
	for i := 0; i < n; i++ {
		idx := int(float64(i) * step)
		s := substantive[idx]
		msgs = append(msgs, fmt.Sprintf("[%s] %s: %s", s.Ts.Format("Jan 02 15:04"), s.Sender, s.Text))
	}
	return msgs
}

const discoverySystemPrompt = `You analyze messaging data to identify distinct ongoing workstreams. Be concise. Respond only with the requested JSON.`

func buildDiscoveryPrompt(digests []conversationDigest) string {
	var b strings.Builder

	b.WriteString(`You are analyzing a workspace's messaging history to identify the distinct WORKSTREAMS — ongoing efforts that span days or weeks. A workstream is a project, feature, bug investigation, deal, partnership, or infrastructure effort. It is NOT a single message, a casual conversation, or a one-off question.

Your job: read the conversation summaries below and identify the coherent workstreams present. A workstream often spans MULTIPLE conversations — the same effort discussed in a DM, a channel, and a group DM.

IMPORTANT:
- Look for efforts that recur across multiple days and conversations.
- DMs with a specific person often represent one or two workstreams (the work you do together).
- Bot channels (#linear-updates, #thunderstorms, @Linear) are NOT workstreams themselves — they are signals that may relate to workstreams.
- Casual coordination ("hey", "call?", "sounds good") is NOT a workstream.
- Each workstream should be a DISTINCT effort. Don't create overlapping workstreams.

CONVERSATIONS:
`)

	for _, d := range digests {
		fmt.Fprintf(&b, "\n--- %s (%d messages, %s) ---\n", d.name, d.signalCount, d.dateRange)
		fmt.Fprintf(&b, "Participants: %s\n", strings.Join(d.participants, ", "))
		for _, msg := range d.messages {
			fmt.Fprintf(&b, "%s\n", msg)
		}
	}

	b.WriteString(`
Respond with a JSON object:
{
  "workstreams": [
    {
      "name": "short descriptive name",
      "focus": "1-3 sentence description of what this workstream is about, mentioning key technical terms, people, and systems",
      "conversations": ["@alice", "#engineering"],
      "participants": ["Alice", "Bob"]
    }
  ]
}

Rules:
- Only include workstreams that represent sustained, coherent efforts.
- A workstream must appear across multiple days OR multiple conversations to qualify.
- List ALL conversations that contribute to each workstream, even partially.
- Be specific in the focus — mention concrete things (PR numbers, service names, feature names) so future signals can be matched.
- Respond with ONLY the JSON object.`)

	return b.String()
}
