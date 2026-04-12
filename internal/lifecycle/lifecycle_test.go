package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock. After returns a channel that fires
// when Advance() has moved time past the requested duration.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, waiter{deadline: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var remaining []waiter
	for _, w := range c.waiters {
		if !w.deadline.After(c.now) {
			w.ch <- c.now
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()
}

// quietLogger returns a logger that discards output, keeping test output clean.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls cond until it returns true or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(1 * time.Millisecond)
	}
	t.Fatalf("condition did not become true within %s", timeout)
}

// blockingListener runs until ctx is cancelled, returning nil.
type blockingListener struct {
	id      string
	started int32
	stopped int32
}

func (l *blockingListener) ID() string { return l.id }
func (l *blockingListener) Run(ctx context.Context) error {
	atomic.AddInt32(&l.started, 1)
	<-ctx.Done()
	atomic.AddInt32(&l.stopped, 1)
	return nil
}

// failingListener returns an error on every run until ctx is cancelled,
// incrementing runs each time Run is entered.
type failingListener struct {
	id   string
	runs int32
	err  error
}

func (l *failingListener) ID() string { return l.id }
func (l *failingListener) Run(ctx context.Context) error {
	atomic.AddInt32(&l.runs, 1)
	if ctx.Err() != nil {
		return nil
	}
	return l.err
}

func TestAddStartsListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, WithLogger(quietLogger()))
	l := &blockingListener{id: "x"}

	if err := sup.Add(l); err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&l.started) == 1 })
	if !sup.Has("x") {
		t.Fatal("supervisor should have id")
	}

	cancel()
	sup.Wait()

	if atomic.LoadInt32(&l.stopped) != 1 {
		t.Fatal("listener should have stopped exactly once")
	}
	if sup.Has("x") {
		t.Fatal("supervisor should no longer have id after shutdown")
	}
}

func TestAddDuplicateIDRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := New(ctx, WithLogger(quietLogger()))

	if err := sup.Add(&blockingListener{id: "dup"}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err := sup.Add(&blockingListener{id: "dup"})
	if !errors.Is(err, ErrAlreadyAdded) {
		t.Fatalf("expected ErrAlreadyAdded, got %v", err)
	}
}

func TestRemoveCancelsAndUnregisters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := New(ctx, WithLogger(quietLogger()))

	l := &blockingListener{id: "r"}
	if err := sup.Add(l); err != nil {
		t.Fatalf("Add: %v", err)
	}
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&l.started) == 1 })

	if err := sup.Remove("r"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if atomic.LoadInt32(&l.stopped) != 1 {
		t.Fatal("listener should have stopped after Remove")
	}
	if sup.Has("r") {
		t.Fatal("supervisor should not have id after Remove")
	}

	if err := sup.Remove("r"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRestartOnErrorWithBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clk := newFakeClock()
	sup := New(ctx,
		WithLogger(quietLogger()),
		WithClock(clk),
		WithBackoff(BackoffPolicy{
			Initial:    10 * time.Millisecond,
			Max:        40 * time.Millisecond,
			Factor:     2.0,
			ResetAfter: time.Hour,
		}),
	)
	l := &failingListener{id: "f", err: errors.New("boom")}
	if err := sup.Add(l); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// First run fires immediately.
	waitFor(t, time.Second, func() bool { return atomic.LoadInt32(&l.runs) >= 1 })

	// Advance past the first backoff (10ms) to trigger the second run.
	waitFor(t, time.Second, func() bool {
		clk.Advance(15 * time.Millisecond)
		return atomic.LoadInt32(&l.runs) >= 2
	})

	// And past the second (20ms) for the third.
	waitFor(t, time.Second, func() bool {
		clk.Advance(25 * time.Millisecond)
		return atomic.LoadInt32(&l.runs) >= 3
	})

	cancel()
	sup.Wait()
}

func TestCleanExitDoesNotRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := New(ctx, WithLogger(quietLogger()))

	var runs int32
	done := make(chan struct{})
	l := ListenerFunc{
		IDValue: "clean",
		RunFunc: func(ctx context.Context) error {
			if atomic.AddInt32(&runs, 1) == 1 {
				close(done)
			}
			return nil // clean exit — must not restart
		},
	}
	if err := sup.Add(l); err != nil {
		t.Fatalf("Add: %v", err)
	}
	<-done

	// Give the supervisor a beat to (incorrectly) restart.
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("expected 1 run, got %d", got)
	}
	if sup.Has("clean") {
		t.Fatal("listener should be unregistered after clean exit")
	}
}

func TestPanicIsRecoveredAndRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clk := newFakeClock()
	sup := New(ctx,
		WithLogger(quietLogger()),
		WithClock(clk),
		WithBackoff(BackoffPolicy{Initial: 1 * time.Millisecond, Max: 1 * time.Millisecond, Factor: 2, ResetAfter: time.Hour}),
	)

	var runs int32
	l := ListenerFunc{
		IDValue: "panicker",
		RunFunc: func(ctx context.Context) error {
			n := atomic.AddInt32(&runs, 1)
			if n < 3 {
				panic("nope")
			}
			<-ctx.Done()
			return nil
		},
	}
	if err := sup.Add(l); err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		clk.Advance(2 * time.Millisecond)
		return atomic.LoadInt32(&runs) >= 3
	})
	cancel()
	sup.Wait()
}

func TestReconcileAddsAndRemoves(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sup := New(ctx, WithLogger(quietLogger()))

	a := &blockingListener{id: "a"}
	b := &blockingListener{id: "b"}
	c := &blockingListener{id: "c"}

	if err := sup.Reconcile([]Listener{a, b}); err != nil {
		t.Fatalf("Reconcile #1: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		return atomic.LoadInt32(&a.started) == 1 && atomic.LoadInt32(&b.started) == 1
	})

	// Drop b, keep a, add c. a's started counter must stay at 1 — it is not
	// restarted when it's already present in desired.
	if err := sup.Reconcile([]Listener{a, c}); err != nil {
		t.Fatalf("Reconcile #2: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		return atomic.LoadInt32(&c.started) == 1 && atomic.LoadInt32(&b.stopped) == 1
	})
	if got := atomic.LoadInt32(&a.started); got != 1 {
		t.Fatalf("a should not restart on reconcile, started=%d", got)
	}

	cancel()
	sup.Wait()
}

func TestAddAfterShutdownReturnsErrStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sup := New(ctx, WithLogger(quietLogger()))
	cancel()
	sup.Wait()

	err := sup.Add(&blockingListener{id: "late"})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("expected ErrStopped, got %v", err)
	}
}
