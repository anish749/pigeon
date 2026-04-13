package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v4"
)

const (
	restartInitialDelay = 5 * time.Second
	restartMaxDelay     = 5 * time.Minute
	restartStableAfter  = 30 * time.Second
)

// runWithRestart runs fn in a loop, restarting it with exponential backoff
// when it exits with an error or unexpectedly returns nil. If fn panics,
// the panic is recovered and treated as a restartable error. The loop exits
// when ctx is cancelled.
//
// The delay starts at 5s and doubles up to 5min. It resets after fn has been
// running for 30+ seconds (indicating a stable run rather than a crash loop).
func runWithRestart(ctx context.Context, label string, fn func(context.Context) error) {
	bo := backoff.NewExponentialBackOff(
		backoff.WithInitialInterval(restartInitialDelay),
		backoff.WithMaxInterval(restartMaxDelay),
		backoff.WithMultiplier(2),
		backoff.WithMaxElapsedTime(0), // never stop retrying
	)

	backoff.RetryNotify(func() error {
		started := time.Now()
		err := runRecoverable(ctx, fn)
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}
		// Reset backoff if the run was stable (lasted 30+ seconds).
		if time.Since(started) >= restartStableAfter {
			bo.Reset()
		}
		if err == nil {
			return fmt.Errorf("unexpected clean exit")
		}
		return err
	}, backoff.WithContext(bo, ctx), func(err error, d time.Duration) {
		slog.Error("account crashed, restarting",
			"account", label, "error", err, "delay", d)
	})
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
