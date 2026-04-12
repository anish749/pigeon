package lifecycle

import (
	"math/rand/v2"
	"time"
)

// RestartPolicy controls how the Supervisor waits between restart attempts
// for a crashed listener. The policy is intentionally simple: exponential
// backoff capped at MaxBackoff, reset to InitialBackoff after the listener
// has run stably for ResetAfter.
//
// A zero RestartPolicy is valid but degenerate (no delay between restarts);
// callers should use DefaultPolicy.
type RestartPolicy struct {
	// InitialBackoff is the first delay after a crash.
	InitialBackoff time.Duration
	// MaxBackoff caps the exponential growth.
	MaxBackoff time.Duration
	// Multiplier is applied to the current backoff after each crash. Must
	// be >= 1; values < 1 are treated as 1.
	Multiplier float64
	// ResetAfter is the minimum uptime required before the backoff is
	// reset to InitialBackoff on the next crash. Zero disables resetting.
	ResetAfter time.Duration
	// Jitter is the fraction (0..1) of random noise added to the backoff.
	// A value of 0.2 means the actual delay is in [backoff, backoff*1.2).
	Jitter float64
}

// DefaultPolicy is a sensible restart policy for network listeners:
// start quickly, back off to a minute at most, reset after a minute of
// stable operation, and add a bit of jitter to avoid thundering herds
// when many listeners crash together (e.g. after a network blip).
var DefaultPolicy = RestartPolicy{
	InitialBackoff: 1 * time.Second,
	MaxBackoff:     60 * time.Second,
	Multiplier:     2.0,
	ResetAfter:     60 * time.Second,
	Jitter:         0.2,
}

// next returns the delay to wait before the next restart given the previous
// backoff and how long the last incarnation ran. It never returns a value
// below InitialBackoff (unless InitialBackoff is zero) or above MaxBackoff.
func (p RestartPolicy) next(prev time.Duration, lastUptime time.Duration) time.Duration {
	// Reset after a stable run.
	if p.ResetAfter > 0 && lastUptime >= p.ResetAfter {
		prev = 0
	}

	next := prev
	if next < p.InitialBackoff {
		next = p.InitialBackoff
	} else {
		mult := p.Multiplier
		if mult < 1 {
			mult = 1
		}
		next = time.Duration(float64(next) * mult)
	}
	if p.MaxBackoff > 0 && next > p.MaxBackoff {
		next = p.MaxBackoff
	}
	if p.Jitter > 0 {
		next += time.Duration(rand.Float64() * p.Jitter * float64(next))
	}
	return next
}
