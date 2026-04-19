package models

import (
	"time"

	"github.com/anish749/pigeon/internal/workspace"
)

// Config holds all configuration for the affinity router system.
// A single instance is created at startup and passed to all components.
type Config struct {
	// Signal reading — time range for replay.
	Since time.Time
	Until time.Time

	// Manager — workstream lifecycle.
	FocusUpdateInterval int           // update focus after this many signals per workstream
	DormancyThreshold   time.Duration // mark workstream dormant after this long without signals
	ApprovalMode        ApprovalMode  // auto-approve or interactive for workstream creation

	// LLM — Claude CLI settings.
	Model          string        // claude model name (e.g. "haiku", "sonnet")
	LLMCallTimeout time.Duration // per-call timeout for each LLM subprocess invocation

	// Filters.
	Workspace workspace.Workspace // resolved workspace — scopes signals to its accounts
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Since:               time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:               time.Now(),
		FocusUpdateInterval: 25,
		DormancyThreshold:   7 * 24 * time.Hour,
		Model:               "haiku",
		LLMCallTimeout:      60 * time.Second,
	}
}
