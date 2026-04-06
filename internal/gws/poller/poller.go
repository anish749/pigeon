package poller

import (
	"context"
	"log/slog"
	"time"
)

// Poller runs periodic polls against Gmail, Drive, and Calendar via the gws CLI.
type Poller struct {
	interval    time.Duration
	cursorsPath string
	cursors     *Cursors
}

// New creates a Poller that ticks at the given interval and persists cursors to path.
func New(interval time.Duration, cursorsPath string) *Poller {
	return &Poller{
		interval:    interval,
		cursorsPath: cursorsPath,
	}
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	var err error
	p.cursors, err = LoadCursors(p.cursorsPath)
	if err != nil {
		return err
	}

	// Seed any missing cursors on first run.
	if err := p.pollAll(ctx); err != nil {
		slog.Error("initial poll failed", "error", err)
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := p.pollAll(ctx); err != nil {
				slog.Error("poll cycle failed", "error", err)
			}
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	slog.Debug("poll cycle starting")

	// Poll each service independently — one failure shouldn't block others.
	var errs []error

	if err := PollGmail(p.cursors); err != nil {
		slog.Error("gmail poll failed", "error", err)
		errs = append(errs, err)
	}

	if err := PollDrive(p.cursors); err != nil {
		slog.Error("drive poll failed", "error", err)
		errs = append(errs, err)
	}

	if err := PollCalendar(p.cursors); err != nil {
		slog.Error("calendar poll failed", "error", err)
		errs = append(errs, err)
	}

	// Persist cursors after every cycle, even if some polls failed —
	// successful polls still advanced their cursors.
	if err := SaveCursors(p.cursorsPath, p.cursors); err != nil {
		slog.Error("save cursors failed", "error", err)
	}

	slog.Debug("poll cycle complete", "errors", len(errs))
	return nil
}
