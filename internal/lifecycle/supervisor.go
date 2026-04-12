package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

// Supervisor owns the goroutines of all registered listeners. It starts
// them lazily on Ensure/Reconcile, restarts crashed ones under a
// RestartPolicy, and stops them on Remove or when its root context is
// cancelled.
//
// A Supervisor is safe for concurrent use from multiple goroutines.
type Supervisor struct {
	policy RestartPolicy
	root   context.Context
	mu     sync.Mutex
	units  map[Key]*unit
	wg     sync.WaitGroup
	closed bool
}

// unit is the internal bookkeeping for one supervised Key. It owns a
// single goroutine that loops Build → Run → wait until the unit's context
// is cancelled.
type unit struct {
	key     Key
	factory Factory
	cancel  context.CancelFunc
	done    chan struct{}
}

// New creates a Supervisor that uses root as the parent context for every
// supervised listener. When root is cancelled, all listeners are stopped
// and no new ones can be started.
func New(root context.Context, policy RestartPolicy) *Supervisor {
	return &Supervisor{
		policy: policy,
		root:   root,
		units:  make(map[Key]*unit),
	}
}

// Ensure registers a Factory and starts its listener if not already
// running. If a listener is already running for the same Key, Ensure is a
// no-op — the existing supervised goroutine keeps running with its
// original Factory. To swap factories, call Remove first.
//
// Returns an error only if the supervisor has already been shut down.
func (s *Supervisor) Ensure(f Factory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("supervisor closed")
	}
	if s.root.Err() != nil {
		return fmt.Errorf("supervisor root context done: %w", s.root.Err())
	}

	key := f.Key()
	if _, ok := s.units[key]; ok {
		return nil
	}

	ctx, cancel := context.WithCancel(s.root)
	u := &unit{
		key:     key,
		factory: f,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.units[key] = u

	s.wg.Add(1)
	go s.supervise(ctx, u)
	return nil
}

// Remove stops the listener for key and waits for it to exit. Safe to
// call for keys that are not currently supervised (no-op).
func (s *Supervisor) Remove(key Key) {
	s.mu.Lock()
	u, ok := s.units[key]
	if ok {
		delete(s.units, key)
	}
	s.mu.Unlock()

	if !ok {
		return
	}
	u.cancel()
	<-u.done
}

// Reconcile adjusts the supervised set to match desired exactly: factories
// whose Key is new are started, keys no longer present are stopped.
// Factories already supervised are left alone (their originally registered
// Factory continues to be used). Reconcile blocks until removed listeners
// have exited.
func (s *Supervisor) Reconcile(desired []Factory) {
	want := make(map[Key]Factory, len(desired))
	for _, f := range desired {
		want[f.Key()] = f
	}

	// Compute removals under lock, then stop outside the lock (Remove blocks).
	s.mu.Lock()
	var toRemove []Key
	for key := range s.units {
		if _, ok := want[key]; !ok {
			toRemove = append(toRemove, key)
		}
	}
	s.mu.Unlock()

	for _, key := range toRemove {
		s.Remove(key)
	}

	for _, f := range desired {
		if err := s.Ensure(f); err != nil {
			slog.Error("supervisor: ensure failed", "key", f.Key(), "error", err)
		}
	}
}

// Keys returns the current supervised keys in sorted order (stable for
// tests and status output).
func (s *Supervisor) Keys() []Key {
	s.mu.Lock()
	keys := make([]Key, 0, len(s.units))
	for k := range s.units {
		keys = append(keys, k)
	}
	s.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		return keys[i].ID < keys[j].ID
	})
	return keys
}

// IDsByKind returns the supervised IDs for a single kind, sorted.
func (s *Supervisor) IDsByKind(kind string) []string {
	var ids []string
	for _, k := range s.Keys() {
		if k.Kind == kind {
			ids = append(ids, k.ID)
		}
	}
	return ids
}

// Shutdown stops accepting new Ensure calls, cancels every supervised
// listener, and waits for all of their goroutines to exit. Safe to call
// once; subsequent calls return immediately.
func (s *Supervisor) Shutdown() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	units := make([]*unit, 0, len(s.units))
	for _, u := range s.units {
		units = append(units, u)
	}
	s.units = make(map[Key]*unit)
	s.mu.Unlock()

	for _, u := range units {
		u.cancel()
	}
	s.wg.Wait()
}

// supervise is the per-listener goroutine. It loops Build → Run with
// exponential backoff, panic recovery, and logging. It exits only when
// u's context is cancelled (via Remove, Shutdown, or root cancellation).
func (s *Supervisor) supervise(ctx context.Context, u *unit) {
	defer s.wg.Done()
	defer close(u.done)

	slog.InfoContext(ctx, "listener supervising", "key", u.key)

	var backoff time.Duration
	for {
		if ctx.Err() != nil {
			return
		}

		started := time.Now()
		err := s.runOnce(ctx, u)
		uptime := time.Since(started)

		if ctx.Err() != nil {
			// Stop was requested — don't log this as a crash.
			slog.InfoContext(ctx, "listener stopped", "key", u.key, "uptime", uptime)
			return
		}

		if err != nil {
			slog.ErrorContext(ctx, "listener crashed",
				"key", u.key, "uptime", uptime, "error", err)
		} else {
			// Clean return without ctx cancellation is unexpected — the
			// listener gave up on its own. Treat as a crash.
			slog.WarnContext(ctx, "listener exited without error, restarting",
				"key", u.key, "uptime", uptime)
		}

		backoff = s.policy.next(backoff, uptime)
		slog.InfoContext(ctx, "listener restart scheduled",
			"key", u.key, "delay", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runOnce builds a fresh listener via the factory and runs it. Panics
// inside Build or Run are converted into errors so the supervisor can
// back off and retry instead of crashing the daemon.
func (s *Supervisor) runOnce(ctx context.Context, u *unit) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()

	listener, err := u.factory.Build(ctx)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	if listener == nil {
		return errors.New("build returned nil listener")
	}
	return listener.Run(ctx)
}
