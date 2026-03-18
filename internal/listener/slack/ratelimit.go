package slack

import (
	"context"
	"log/slog"
	"sync"
	"time"

	goslack "github.com/slack-go/slack"
)

// rateLimitGate blocks callers until a rate limit window has passed.
// When a 429 is received, call update() with the retry-after duration.
// Before each API call, call wait() to block until the window expires.
type rateLimitGate struct {
	mu        sync.Mutex
	deadline  time.Time
	workspace string
}

// wait blocks until the current rate limit window has passed, or ctx is cancelled.
func (g *rateLimitGate) wait(ctx context.Context) error {
	g.mu.Lock()
	deadline := g.deadline
	g.mu.Unlock()

	wait := time.Until(deadline)
	if wait <= 0 {
		return nil
	}

	slog.InfoContext(ctx, "slack sync: rate limited, waiting", "workspace", g.workspace, "duration", wait.Round(time.Second))
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		slog.InfoContext(ctx, "slack sync: rate limit wait done, resuming", "workspace", g.workspace)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// update sets the gate deadline if a RateLimitedError is detected.
// Returns true if the error was a rate limit (caller should retry).
// Returns false for all other errors (caller should handle normally).
func (g *rateLimitGate) update(err error) bool {
	if err == nil {
		return false
	}
	rle, ok := err.(*goslack.RateLimitedError)
	if !ok {
		return false
	}

	g.mu.Lock()
	newDeadline := time.Now().Add(rle.RetryAfter)
	if newDeadline.After(g.deadline) {
		g.deadline = newDeadline
	}
	g.mu.Unlock()
	return true
}
