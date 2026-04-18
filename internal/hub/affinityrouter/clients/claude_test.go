package clients

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain JSON",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "json code fence",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "plain code fence",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "trailing whitespace",
			input: "  ```json\n{\"key\": \"value\"}\n```  ",
			want:  `{"key": "value"}`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeFences(tt.input)
			if got != tt.want {
				t.Errorf("stripCodeFences(%q)\ngot  %q\nwant %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestClient_Classify calls the real claude CLI. Skip unless CLAUDE_LIVE_TEST=1.
//
// Run with: CLAUDE_LIVE_TEST=1 go test ./internal/hub/affinityrouter/clients/ -run TestClient_Classify -v
func TestClient_Classify(t *testing.T) {
	if os.Getenv("CLAUDE_LIVE_TEST") == "" {
		t.Skip("set CLAUDE_LIVE_TEST=1 to run live claude CLI test")
	}

	c := New("haiku", 120*time.Second, slog.Default())

	// Prompt crafted to reliably route to the provided workstream.
	prompt := `You are a workstream classifier. Classify the messages below against the active workstreams.

Workspace: test
Conversation: #eng

Active workstreams:
- ID: ws-infra
  Name: Infrastructure
  Focus: Server reliability, deployments, and on-call incidents.
  Signals so far: 12

Messages to classify:
[2026-01-10 14:00] Alice: The deploy pipeline is failing again, same error as last week.
[2026-01-10 14:02] Bob: I'll take a look, probably the same flaky test blocking the rollout.

Respond with a JSON object:
{
  "workstreams": ["<workstream_id>"],
  "new_workstream_name": "<short name for new workstream, only if proposing new>",
  "new_workstream_focus": "<1-3 sentence description, only if proposing new>",
  "confidence": <0.0 to 1.0>,
  "reasoning": "<brief explanation>"
}
Respond with ONLY the JSON object, no other text.`

	resp, err := c.Classify(context.Background(), prompt)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if resp.Confidence < 0 || resp.Confidence > 1 {
		t.Errorf("Confidence %f out of [0,1]", resp.Confidence)
	}
	if resp.Reasoning == "" {
		t.Error("Reasoning is empty")
	}

	t.Logf("routed to workstreams=%v new_name=%q confidence=%.2f reasoning=%s",
		resp.Workstreams, resp.NewWorkstreamName, resp.Confidence, resp.Reasoning)
}

// TestClient_UpdateFocus calls the real claude CLI. Skip unless CLAUDE_LIVE_TEST=1.
//
// Run with: CLAUDE_LIVE_TEST=1 go test ./internal/hub/affinityrouter/clients/ -run TestClient_UpdateFocus -v
func TestClient_UpdateFocus(t *testing.T) {
	if os.Getenv("CLAUDE_LIVE_TEST") == "" {
		t.Skip("set CLAUDE_LIVE_TEST=1 to run live claude CLI test")
	}

	c := New("haiku", 120*time.Second, slog.Default())

	prompt := `Update the focus description for a workstream called "Infrastructure" that tracks server reliability and on-call incidents.

New signals received:
- Deploy pipeline broke due to a flaky integration test.
- On-call rotation was triggered for a memory leak in the auth service.

Current focus: "Server reliability, deployments, and on-call incidents."

Write an updated 1-3 sentence focus description that incorporates the new signals. Reply with the description only, no other text.`

	focus, err := c.UpdateFocus(context.Background(), prompt)
	if err != nil {
		t.Fatalf("UpdateFocus: %v", err)
	}
	if focus == "" {
		t.Error("focus is empty")
	}

	t.Logf("updated focus: %s", focus)
}
