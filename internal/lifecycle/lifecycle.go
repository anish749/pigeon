// Package lifecycle provides a Supervisor that manages the lifetime of
// long-running listeners (Slack socket mode, WhatsApp event loops, pollers,
// etc.). It is the single place where per-listener restart, backoff, panic
// recovery, and reconciliation against a desired configuration live.
//
// A Listener is anything that runs forever against a context: it blocks in
// Run until ctx is cancelled (clean shutdown — returns nil and is not
// restarted) or until it hits a fatal condition (returns a non-nil error —
// Supervisor restarts it with exponential backoff).
//
// The Supervisor itself does not know anything about Slack, WhatsApp or
// pollers. Platform-specific code constructs Listener values and hands them
// to the Supervisor via Add / Reconcile.
package lifecycle
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Listener is a single supervised unit of work. Implementations must make
// Run block until ctx is cancelled or a fatal error occurs.
//
//   - Returning nil signals "I am done, do not restart me" (e.g. the account
//     was logged out, or ctx.Err() != nil).
//   - Returning a non-nil error signals a crash the Supervisor should recover
//     from by restarting Run with a fresh ctx after a backoff delay.
//   - A panic inside Run is treated as a non-nil error.
//
// ID must be stable for the lifetime of the listener and unique within a
// Supervisor; it is used for logs, Reconcile diffs, and Remove lookups.
type Listener interface {
	ID() string
	Run(ctx context.Context) error
}

// BackoffPolicy controls how the Supervisor spaces restarts after a Listener
// returns an error.
type BackoffPolicy struct {
	// Initial is the delay before the first restart.
	Initial time.Duration
	// Max caps the delay regardless of how many times a Listener has failed.
	Max time.Duration
	// Factor multiplies the delay between consecutive failures (>= 1).
	Factor float64
	// ResetAfter: if a Listener stayed up at least this long before failing,
	// its backoff is reset to Initial on the next restart. This prevents a
	// listener that reconnected successfully for hours from waiting Max
	// before its next retry.
	ResetAfter time.Duration
}

// DefaultBackoff is a reasonable default for network-bound listeners.
var DefaultBackoff = BackoffPolicy{
	Initial:    1 * time.Second,
	Max:        2 * time.Minute,
	Factor:     2.0,
	ResetAfter: 1 * time.Minute,
}

// Clock abstracts time for tests. The zero value uses the real clock.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Option configures a Supervisor at construction time.
type Option func(*Supervisor)

// WithBackoff overrides the default BackoffPolicy.
func WithBackoff(p BackoffPolicy) Option {
	return func(s *Supervisor) { s.backoff = p }
}

// WithLogger overrides the default slog logger.
func WithLogger(l *slog.Logger) Option {
	return func(s *Supervisor) { s.logger = l }
}

// WithClock overrides the clock (for tests).
func WithClock(c Clock) Option {
	return func(s *Supervisor) { s.clock = c }
}

// Supervisor runs Listeners, restarts them on error with exponential backoff,
// and supports dynamic add / remove / reconcile.
//
// A Supervisor is bound to a root context at construction. When that context
// is cancelled, every supervised Listener is cancelled and Wait returns once
// they have all stopped. After shutdown begins the Supervisor rejects new
// Add / Reconcile calls.
type Supervisor struct {
	backoff BackoffPolicy
	clock   Clock
	logger  *slog.Logger

	rootCtx context.Context

	mu      sync.Mutex
	entries map[string]*entry
	wg      sync.WaitGroup
}

type entry struct {
	listener Listener
	cancel   context.CancelFunc
	done     chan struct{}
}

// New creates a Supervisor bound to ctx. When ctx is cancelled the Supervisor
// stops accepting new listeners and all running listeners are cancelled.
func New(ctx context.Context, opts ...Option) *Supervisor {
	s := &Supervisor{
		backoff: DefaultBackoff,
		clock:   realClock{},
		logger:  slog.Default(),
		rootCtx: ctx,
		entries: make(map[string]*entry),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// stopped reports whether the root context has been cancelled.
func (s *Supervisor) isStopped() bool {
	return s.rootCtx.Err() != nil
}

// ErrStopped is returned by Add / Reconcile / Remove after the Supervisor's
// root context has been cancelled.
var ErrStopped = errors.New("lifecycle: supervisor is stopped")

// ErrAlreadyAdded is returned by Add when a Listener with the same ID is
// already supervised.
var ErrAlreadyAdded = errors.New("lifecycle: listener already added")

// ErrNotFound is returned by Remove when no Listener with the given ID is
// supervised.
var ErrNotFound = errors.New("lifecycle: listener not found")

// Add begins supervising l. It returns ErrAlreadyAdded if a listener with
// the same ID is already present, or ErrStopped if the Supervisor has been
// shut down.
func (s *Supervisor) Add(l Listener) error {
	s.mu.Lock()
	if s.isStopped() {
		s.mu.Unlock()
		return ErrStopped
	}
	id := l.ID()
	if _, exists := s.entries[id]; exists {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrAlreadyAdded, id)
	}

	ctx, cancel := context.WithCancel(s.rootCtx)
	e := &entry{
		listener: l,
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	s.entries[id] = e
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer close(e.done)
		s.supervise(ctx, l)

		// Remove from registry once the supervised loop exits. Under
		// Remove() this is redundant (Remove deletes first, then cancels),
		// but when Run returns nil on its own we still need to clean up.
		s.mu.Lock()
		if cur, ok := s.entries[id]; ok && cur == e {
			delete(s.entries, id)
		}
		s.mu.Unlock()
	}()
	return nil
}

// Remove cancels the Listener with the given ID and waits for it to stop.
// Returns ErrNotFound if no such listener is supervised.
func (s *Supervisor) Remove(id string) error {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(s.entries, id)
	s.mu.Unlock()

	e.cancel()
	<-e.done
	return nil
}

// Reconcile brings the set of supervised Listeners in line with desired: it
// adds Listeners whose IDs are not currently supervised and removes
// Listeners whose IDs are not in desired. Listeners already running with a
// matching ID are left untouched — their existing Run loops keep going.
//
// Errors from individual Add / Remove calls are collected and returned
// joined; a partial reconcile is still applied.
func (s *Supervisor) Reconcile(desired []Listener) error {
	desiredByID := make(map[string]Listener, len(desired))
	for _, l := range desired {
		desiredByID[l.ID()] = l
	}

	// Snapshot current IDs under the lock.
	s.mu.Lock()
	if s.isStopped() {
		s.mu.Unlock()
		return ErrStopped
	}
	current := make([]string, 0, len(s.entries))
	for id := range s.entries {
		current = append(current, id)
	}
	s.mu.Unlock()

	var errs []error
	for _, id := range current {
		if _, keep := desiredByID[id]; keep {
			continue
		}
		if err := s.Remove(id); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("remove %s: %w", id, err))
		}
	}
	for _, l := range desired {
		if err := s.Add(l); err != nil && !errors.Is(err, ErrAlreadyAdded) {
			errs = append(errs, fmt.Errorf("add %s: %w", l.ID(), err))
		}
	}
	return errors.Join(errs...)
}

// IDs returns the IDs of all currently supervised Listeners.
func (s *Supervisor) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.entries))
	for id := range s.entries {
		ids = append(ids, id)
	}
	return ids
}

// Has reports whether a Listener with the given ID is supervised.
func (s *Supervisor) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.entries[id]
	return ok
}

// Wait blocks until the root context is cancelled and every supervised
// Listener has stopped.
func (s *Supervisor) Wait() {
	<-s.rootCtx.Done()
	s.wg.Wait()
}

// supervise runs l.Run in a loop, restarting it with exponential backoff
// whenever it returns a non-nil error or panics. It returns once ctx is
// cancelled or Run returns nil (indicating clean, intentional shutdown).
func (s *Supervisor) supervise(ctx context.Context, l Listener) {
	id := l.ID()
	delay := s.backoff.Initial

	for {
		if ctx.Err() != nil {
			return
		}

		startedAt := s.clock.Now()
		err := runOnce(ctx, l)
		ranFor := s.clock.Now().Sub(startedAt)

		if ctx.Err() != nil {
			// Shutdown in progress — swallow any error, it is almost
			// always "context canceled".
			return
		}
		if err == nil {
			s.logger.Info("lifecycle: listener exited cleanly",
				"id", id, "ran_for", ranFor)
			return
		}

		if ranFor >= s.backoff.ResetAfter {
			delay = s.backoff.Initial
		}

		s.logger.Error("lifecycle: listener failed, restarting",
			"id", id, "error", err, "ran_for", ranFor, "backoff", delay)

		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(delay):
		}

		next := time.Duration(float64(delay) * s.backoff.Factor)
		if next > s.backoff.Max {
			next = s.backoff.Max
		}
		if next <= delay {
			// Guard against Factor <= 1 or overflow.
			next = s.backoff.Max
		}
		delay = next
	}
}

// runOnce calls l.Run and converts any panic into an error.
func runOnce(ctx context.Context, l Listener) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return l.Run(ctx)
}

// ListenerFunc adapts a plain function into a Listener. id is captured at
// construction time.
type ListenerFunc struct {
	IDValue string
	RunFunc func(ctx context.Context) error
}

// ID returns the stored ID.
func (f ListenerFunc) ID() string { return f.IDValue }

// Run invokes the stored function.
func (f ListenerFunc) Run(ctx context.Context) error { return f.RunFunc(ctx) }
