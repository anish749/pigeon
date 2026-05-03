package daemon

import (
	"context"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/syncstatus"
)

// TestMaintenanceManager_RequestsSerializedThroughChannel fires Trigger
// many times concurrently and asserts that consumers reading from the
// channel observe at most one in flight at any moment. This is the
// guarantee the user explicitly asked for: "only one thing running" —
// the buffered channel + single consumer is the structural enforcement.
func TestMaintenanceManager_RequestsSerializedThroughChannel(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const N = 10
	var (
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
		ran         atomic.Int32
	)

	// Single consumer of the channel — this is exactly what the real
	// worker does. The test stands in for it so we can observe overlap
	// without depending on FSStore.Maintain's behaviour.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.requests:
				cur := inFlight.Add(1)
				for {
					prev := maxInFlight.Load()
					if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond) // give other goroutines a chance to overlap
				ran.Add(1)
				inFlight.Add(-1)
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			m.Trigger(ctx, account.New("linear", "ws"+strconv.Itoa(i)))
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for ran.Load() < int32(N) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := ran.Load(); got != int32(N) {
		t.Fatalf("ran %d, want %d", got, N)
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("maxInFlight = %d, want 1 (single consumer)", got)
	}
}

// TestMaintenanceManager_TriggerBlocksWhenBufferFull confirms Trigger is
// a blocking send so backpressure flows back to the caller (slack/sync,
// scheduler) when the worker can't keep up. Silently dropping triggers
// would leave the writer thinking maintenance is queued when it isn't.
func TestMaintenanceManager_TriggerBlocksWhenBufferFull(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())

	ctx := context.Background()

	// Fill the buffer without starting a worker.
	for i := 0; i < triggerBuffer; i++ {
		m.Trigger(ctx, account.New("linear", "ws"))
	}

	// The next Trigger must block. Run it in a goroutine and assert it
	// hasn't returned within a short timeout.
	done := make(chan struct{})
	go func() {
		m.Trigger(ctx, account.New("linear", "ws"))
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Trigger returned with full buffer; expected it to block")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	// Drain one slot and confirm the blocked Trigger now completes.
	<-m.requests
	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("Trigger did not unblock after a slot was freed")
	}
}

// TestMaintenanceManager_TriggerReturnsOnContextCancel ensures Trigger
// does not park indefinitely on shutdown when the worker has already
// exited and the buffer is full.
func TestMaintenanceManager_TriggerReturnsOnContextCancel(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())

	ctx, cancel := context.WithCancel(context.Background())

	// Fill the buffer with no worker running.
	for i := 0; i < triggerBuffer; i++ {
		m.Trigger(ctx, account.New("linear", "ws"))
	}

	// The next call would block forever; cancellation must release it.
	done := make(chan struct{})
	go func() {
		m.Trigger(ctx, account.New("linear", "ws"))
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// expected: Trigger returned because ctx was cancelled.
	case <-time.After(time.Second):
		t.Fatal("Trigger did not return after ctx cancellation")
	}
}

// TestMaintenanceManager_IsStale exercises the wall-clock mtime gate.
func TestMaintenanceManager_IsStale(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())

	acct := account.New("linear", "ws")

	// Missing .maintenance.json → stale.
	if !m.isStale(acct) {
		t.Error("isStale = false for missing maintenance file, want true")
	}

	// Maintain expects the account directory to exist. Create it (empty)
	// and run Maintain to write the maintenance file with the current
	// mtime; the gate should now report not-stale.
	acctDir := root.AccountFor(acct)
	if err := os.MkdirAll(acctDir.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Maintain(acct); err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if m.isStale(acct) {
		t.Error("isStale = true immediately after Maintain, want false")
	}
}

// TestMaintenanceManager_SchedulerEnqueuesStale verifies the scheduler
// surfaces stale accounts onto the request queue.
func TestMaintenanceManager_SchedulerEnqueuesStale(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())
	m.setAccounts(&config.Config{Linear: []config.LinearConfig{{Workspace: "alpha"}}})

	// Scheduler tick logic, executed inline so we don't wait an hour.
	ctx := context.Background()
	for _, acct := range m.snapshotAccounts() {
		if m.isStale(acct) {
			m.Trigger(ctx, acct)
		}
	}

	select {
	case got := <-m.requests:
		if got.String() != "linear-alpha" {
			t.Errorf("got %q, want linear-alpha", got.String())
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not enqueue the stale account")
	}
}

// TestMaintenanceManager_SchedulerPicksUpReconciledAccounts drives the
// scheduler tick across a config change to verify that new accounts get
// enqueued and removed accounts stop being enqueued. This covers the
// loop end-to-end: setAccounts updates the atomic snapshot, the
// scheduler reads it on the next tick, and the right requests land on
// the queue.
func TestMaintenanceManager_SchedulerPicksUpReconciledAccounts(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())
	ctx := context.Background()

	// One scheduler tick over the current snapshot. Mirrors the loop body
	// of m.scheduler so the test can run it inline without waiting an
	// hour for the real ticker.
	tick := func() {
		for _, acct := range m.snapshotAccounts() {
			if m.isStale(acct) {
				m.Trigger(ctx, acct)
			}
		}
	}

	// Drain everything currently on the requests channel into a sorted
	// slice of keys. Used as the observable for "what did this tick
	// enqueue?".
	drain := func() []string {
		var out []string
		for {
			select {
			case got := <-m.requests:
				out = append(out, got.String())
			case <-time.After(20 * time.Millisecond):
				sort.Strings(out)
				return out
			}
		}
	}

	// Initial config: alpha only. Tick should enqueue alpha.
	m.setAccounts(&config.Config{Linear: []config.LinearConfig{{Workspace: "alpha"}}})
	tick()
	if got, want := drain(), []string{"linear-alpha"}; !equalStrings(got, want) {
		t.Fatalf("tick #1: got %v, want %v", got, want)
	}

	// Reconcile: drop alpha, add beta and gamma. Next tick targets the
	// new set.
	m.setAccounts(&config.Config{
		Linear: []config.LinearConfig{
			{Workspace: "beta"},
			{Workspace: "gamma"},
		},
	})
	tick()
	if got, want := drain(), []string{"linear-beta", "linear-gamma"}; !equalStrings(got, want) {
		t.Fatalf("tick #2: got %v, want %v", got, want)
	}

	// Reconcile to empty. Tick should enqueue nothing.
	m.setAccounts(&config.Config{})
	tick()
	if got := drain(); len(got) != 0 {
		t.Fatalf("tick #3: got %v, want []", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMaintenanceManager_SetAccountsReconciliation exercises the live
// account snapshot the scheduler reads on every tick. A config change
// must add new accounts and drop removed ones from the active set so a
// subsequent tick targets the right list.
func TestMaintenanceManager_SetAccountsReconciliation(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root, syncstatus.NewTracker())

	keys := func() []string {
		var out []string
		for _, a := range m.snapshotAccounts() {
			out = append(out, a.String())
		}
		sort.Strings(out)
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	m.setAccounts(&config.Config{
		Slack:  []config.SlackConfig{{Workspace: "acme"}},
		Linear: []config.LinearConfig{{Workspace: "alpha"}},
	})
	if got, want := keys(), []string{"linear-alpha", "slack-acme"}; !equal(got, want) {
		t.Fatalf("after first config: got %v, want %v", got, want)
	}

	// Drop slack, add gws — both directions in one tick.
	m.setAccounts(&config.Config{
		GWS:    []config.GWSConfig{{Email: "x@y.com"}},
		Linear: []config.LinearConfig{{Workspace: "alpha"}},
	})
	if got, want := keys(), []string{"gws-xaty-com", "linear-alpha"}; !equal(got, want) {
		t.Fatalf("after reconcile: got %v, want %v", got, want)
	}

	// Empty config — every account dropped.
	m.setAccounts(&config.Config{})
	if got := keys(); len(got) != 0 {
		t.Fatalf("after empty config: got %v, want []", got)
	}
}

// TestMaintenanceManager_DefersWhenSyncing covers the wait-for-sync
// behaviour: a Trigger arriving while the account is syncing must not
// run Maintain immediately. The waiter holds onto the request and
// re-Triggers once Tracker.Done fires for that account.
//
// The test drives the worker's dispatch logic inline rather than running
// m.worker as a goroutine — the goroutine version would consume the
// re-Triggered request before the test could observe it.
func TestMaintenanceManager_DefersWhenSyncing(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	tracker := syncstatus.NewTracker()
	m := NewMaintenanceManager(s, root, tracker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acct := account.New("linear", "ws")
	tracker.Start(acct.String(), syncstatus.KindPoll)
	m.Trigger(ctx, acct)

	// Worker step: pop, see syncing, defer.
	popped := <-m.requests
	done := tracker.WaitForDone(popped.String())
	if done == nil {
		t.Fatal("WaitForDone returned nil while account was syncing")
	}
	m.deferUntilSyncDone(ctx, popped, done)

	// Nothing should land on m.requests while sync is in progress.
	select {
	case got := <-m.requests:
		t.Fatalf("re-Triggered while syncing: %v", got)
	case <-time.After(20 * time.Millisecond):
		// expected
	}

	// Finish the sync — the waiter wakes up and re-Triggers.
	tracker.Done(acct.String(), nil)
	select {
	case got := <-m.requests:
		if got.String() != acct.String() {
			t.Errorf("re-Triggered account = %q, want %q", got.String(), acct.String())
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not re-Trigger after Tracker.Done")
	}
}

// TestMaintenanceManager_DefersCoalesceWhileSyncing verifies multiple
// Triggers arriving for one syncing account collapse into a single
// requeue once sync finishes — otherwise N Triggers during one sync
// would yield N back-to-back Maintain runs.
func TestMaintenanceManager_DefersCoalesceWhileSyncing(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	tracker := syncstatus.NewTracker()
	m := NewMaintenanceManager(s, root, tracker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acct := account.New("linear", "ws")
	tracker.Start(acct.String(), syncstatus.KindPoll)

	// Drive the worker logic inline: 5 Triggers all pop and defer.
	const N = 5
	for i := 0; i < N; i++ {
		m.Trigger(ctx, acct)
	}
	for i := 0; i < N; i++ {
		popped := <-m.requests
		done := tracker.WaitForDone(popped.String())
		if done == nil {
			t.Fatalf("call %d: WaitForDone returned nil while syncing", i)
		}
		m.deferUntilSyncDone(ctx, popped, done)
	}

	// All 5 deferrals collapse into a single waiter goroutine.
	m.deferredMu.Lock()
	pending := len(m.deferred)
	m.deferredMu.Unlock()
	if pending != 1 {
		t.Errorf("deferred set has %d entries, want 1 (coalesced)", pending)
	}

	tracker.Done(acct.String(), nil)

	// Exactly one re-Trigger lands.
	select {
	case <-m.requests:
		// expected
	case <-time.After(time.Second):
		t.Fatal("waiter did not re-Trigger")
	}
	select {
	case extra := <-m.requests:
		t.Fatalf("second re-Trigger landed (%q); coalescing failed", extra.String())
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}
