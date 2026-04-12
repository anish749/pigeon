package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fastPolicy is a restart policy tuned for tests: tiny delays, no jitter.
var fastPolicy = RestartPolicy{
	InitialBackoff: time.Millisecond,
	MaxBackoff:     2 * time.Millisecond,
	Multiplier:     2,
	ResetAfter:     50 * time.Millisecond,
}

type countingFactory struct {
	key    Key
	runFn  func(ctx context.Context, attempt int) error
	builds int32
}

func (f *countingFactory) Key() Key { return f.key }

func (f *countingFactory) Build(_ context.Context) (Listener, error) {
	attempt := atomic.AddInt32(&f.builds, 1)
	run := f.runFn
	return ListenerFunc(func(ctx context.Context) error {
		return run(ctx, int(attempt))
	}), nil
}

func (f *countingFactory) Builds() int { return int(atomic.LoadInt32(&f.builds)) }

func TestSupervisor_RestartsOnCrash(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)
	defer sup.Shutdown()

	done := make(chan struct{})
	f := &countingFactory{
		key: Key{Kind: "test", ID: "crasher"},
		runFn: func(ctx context.Context, attempt int) error {
			if attempt >= 3 {
				close(done)
				<-ctx.Done()
				return ctx.Err()
			}
			return errors.New("boom")
		},
	}
	if err := sup.Ensure(f); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("listener never reached 3rd attempt (builds=%d)", f.Builds())
	}
	if got := f.Builds(); got < 3 {
		t.Errorf("builds = %d, want >= 3", got)
	}
}

func TestSupervisor_RecoversFromPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)
	defer sup.Shutdown()

	done := make(chan struct{})
	f := &countingFactory{
		key: Key{Kind: "test", ID: "panicker"},
		runFn: func(ctx context.Context, attempt int) error {
			if attempt == 1 {
				panic("kaboom")
			}
			close(done)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	if err := sup.Ensure(f); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("listener never recovered from panic (builds=%d)", f.Builds())
	}
}

func TestSupervisor_RemoveStopsListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)
	defer sup.Shutdown()

	running := make(chan struct{})
	f := &countingFactory{
		key: Key{Kind: "test", ID: "long"},
		runFn: func(ctx context.Context, _ int) error {
			select {
			case running <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	if err := sup.Ensure(f); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	<-running

	sup.Remove(f.Key())

	if keys := sup.Keys(); len(keys) != 0 {
		t.Errorf("after Remove: keys = %v, want empty", keys)
	}
}

func TestSupervisor_EnsureIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)
	defer sup.Shutdown()

	started := make(chan struct{}, 2)
	f := &countingFactory{
		key: Key{Kind: "test", ID: "once"},
		runFn: func(ctx context.Context, _ int) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	if err := sup.Ensure(f); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := sup.Ensure(f); err != nil {
		t.Fatalf("Ensure (second): %v", err)
	}

	// Only one start should fire within a reasonable window.
	<-started
	select {
	case <-started:
		t.Fatalf("listener built twice for same key")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSupervisor_Reconcile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)
	defer sup.Shutdown()

	makeFactory := func(id string, started chan<- string) Factory {
		return &countingFactory{
			key: Key{Kind: "test", ID: id},
			runFn: func(ctx context.Context, _ int) error {
				select {
				case started <- id:
				default:
				}
				<-ctx.Done()
				return ctx.Err()
			},
		}
	}

	startedA := make(chan string, 1)
	startedB := make(chan string, 1)
	sup.Reconcile([]Factory{
		makeFactory("a", startedA),
		makeFactory("b", startedB),
	})
	<-startedA
	<-startedB

	// Drop b, add c.
	startedC := make(chan string, 1)
	sup.Reconcile([]Factory{
		makeFactory("a", startedA),
		makeFactory("c", startedC),
	})
	<-startedC

	keys := sup.IDsByKind("test")
	want := map[string]bool{"a": true, "c": true}
	if len(keys) != 2 {
		t.Fatalf("ids = %v, want len 2", keys)
	}
	for _, id := range keys {
		if !want[id] {
			t.Errorf("unexpected id %q running", id)
		}
	}
}

func TestSupervisor_ShutdownStopsAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := New(ctx, fastPolicy)

	started := make(chan struct{}, 3)
	for _, id := range []string{"a", "b", "c"} {
		f := FactoryFunc{
			K: Key{Kind: "test", ID: id},
			BuildFn: func(_ context.Context) (Listener, error) {
				return ListenerFunc(func(ctx context.Context) error {
					started <- struct{}{}
					<-ctx.Done()
					return ctx.Err()
				}), nil
			},
		}
		if err := sup.Ensure(f); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
	}
	// Wait until all three are actually inside Run before shutting down.
	for range 3 {
		<-started
	}

	sup.Shutdown()

	if keys := sup.Keys(); len(keys) != 0 {
		t.Errorf("after Shutdown: keys = %v, want empty", keys)
	}
	if err := sup.Ensure(FactoryFunc{
		K:       Key{Kind: "test", ID: "late"},
		BuildFn: func(_ context.Context) (Listener, error) { return nil, nil },
	}); err == nil {
		t.Errorf("Ensure after Shutdown: want error, got nil")
	}
}

func TestRestartPolicy_Progression(t *testing.T) {
	p := RestartPolicy{
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		Multiplier:     2,
		ResetAfter:     time.Second,
	}
	// First crash: start at initial.
	d := p.next(0, 0)
	if d != 10*time.Millisecond {
		t.Errorf("first backoff = %v, want 10ms", d)
	}
	// Grows.
	d = p.next(d, 0)
	if d != 20*time.Millisecond {
		t.Errorf("second backoff = %v, want 20ms", d)
	}
	// Caps.
	d = p.next(80*time.Millisecond, 0)
	if d != 100*time.Millisecond {
		t.Errorf("capped backoff = %v, want 100ms", d)
	}
	// Resets after stable run.
	d = p.next(100*time.Millisecond, 2*time.Second)
	if d != 10*time.Millisecond {
		t.Errorf("reset backoff = %v, want 10ms", d)
	}
}
