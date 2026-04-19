// Package clients provides LLM clients for the affinity router.
package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Client wraps the claude CLI for non-interactive LLM calls.
type Client struct {
	model   string
	timeout time.Duration
	logger  *slog.Logger
}

// New creates a Claude CLI client.
func New(model string, timeout time.Duration, logger *slog.Logger) *Client {
	return &Client{model: model, timeout: timeout, logger: logger}
}

// cliEnvelope is the outer JSON wrapper returned by claude --output-format json.
type cliEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Text returns the assistant reply as plain text (the envelope's result string).
// systemPrompt replaces the default Claude Code system prompt to reduce token usage.
func (c *Client) Text(ctx context.Context, systemPrompt, prompt string) (string, error) {
	result, err := c.run(ctx, systemPrompt, prompt)
	if err != nil {
		return "", fmt.Errorf("text: %w", err)
	}
	return strings.TrimSpace(result), nil
}

// JSON unmarshals the assistant reply (the envelope's result string) into out.
// systemPrompt replaces the default Claude Code system prompt to reduce token usage.
func (c *Client) JSON(ctx context.Context, systemPrompt, prompt string, out any) error {
	result, err := c.run(ctx, systemPrompt, prompt)
	if err != nil {
		return fmt.Errorf("json: %w", err)
	}
	cleaned := stripCodeFences(result)
	if err := json.Unmarshal([]byte(cleaned), out); err != nil {
		return fmt.Errorf("json: parse response: %w", err)
	}
	return nil
}

// run executes the claude CLI and returns the result text from the response envelope.
func (c *Client) run(ctx context.Context, systemPrompt, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{
		"-p",
		"--model", c.model,
		"--output-format", "json",
		"--no-session-persistence",
		"--tools", "",
		"--setting-sources", "",
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	args = append(args, "--", prompt)
	cmd := exec.CommandContext(ctx, "claude", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timed out after %s", c.timeout)
		}
		return "", fmt.Errorf("claude CLI: %w: %s", err, stderr.String())
	}

	var env cliEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		return "", fmt.Errorf("parse CLI output: %w", err)
	}
	if env.IsError {
		return "", fmt.Errorf("claude CLI error: %s", env.Result)
	}
	c.logger.Info("claude cli response", "raw", json.RawMessage(stdout.Bytes()))
	return env.Result, nil
}

// stripCodeFences removes markdown code fences from a string.
// Handles ```json\n...\n```, ```\n...\n```, and leading/trailing whitespace.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence (```json or ```)
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	}
	// Remove closing fence
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
