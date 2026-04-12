package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	restartInitialDelay = 5 * time.Second
	restartMaxDelay     = 5 * time.Minute
	restartStableAfter  = 30 * time.Second
)

// runWithRestart runs fn in a loop, restarting it with exponential backoff
// when it exits with an error. If fn panics, the panic is recovered and
// treated as a restartable error. The loop exits when ctx is cancelled.
//
// The delay starts at 5s and doubles up to 5min. It resets to 5s after fn
// has been running for 30+ seconds (indicating a stable run rather than an
// immediate crash loop).
func runWithRestart(ctx context.Context, label string, fn func(context.Context) error) {
	delay := restartInitialDelay

	for {
		started := time.Now()
		err := runRecoverable(ctx, fn)

		// Context cancelled — daemon is shutting down.
		if ctx.Err() != nil {
			return
		}

		if err == nil {
			// fn returned nil but context is still alive — unexpected
			// clean exit. Treat it the same as a crash.
			slog.Warn("account exited unexpectedly, restarting",
				"account", label, "delay", delay)
		} else {
			slog.Error("account crashed, restarting",
				"account", label, "error", err, "delay", delay)
		}

		// Reset backoff if the run was stable (lasted 30+ seconds).
		if time.Since(started) >= restartStableAfter {
			delay = restartInitialDelay
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		// Exponential backoff, capped at max.
		delay = min(delay*2, restartMaxDelay)
	}
}

// runRecoverable calls fn, recovering from panics and returning them as errors.
func runRecoverable(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(ctx)
}
