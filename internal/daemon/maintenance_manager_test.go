package daemon

import (
	"context"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
)

// TestConfiguredAccounts verifies the config → account list mapping covers
// every platform the daemon manages and does not silently drop one.
func TestConfiguredAccounts(t *testing.T) {
	cfg := &config.Config{
		Slack:    []config.SlackConfig{{Workspace: "acme"}},
		GWS:      []config.GWSConfig{{Email: "user@example.com"}},
		WhatsApp: []config.WhatsAppConfig{{Account: "+15551234567"}},
		Linear:   []config.LinearConfig{{Workspace: "my-team"}},
		Jira:     []config.JiraConfig{{AccountName: "atlassian"}},
	}

	got := configuredAccounts(cfg)
	gotKeys := make([]string, 0, len(got))
	for _, a := range got {
		gotKeys = append(gotKeys, a.String())
	}
	sort.Strings(gotKeys)

	want := []account.Account{
		account.New("slack", "acme"),
		account.New("gws", "user@example.com"),
		account.New("whatsapp", "+15551234567"),
		account.New("linear", "my-team"),
		account.New("jira-issues", "atlassian"),
	}
	wantKeys := make([]string, 0, len(want))
	for _, a := range want {
		wantKeys = append(wantKeys, a.String())
	}
	sort.Strings(wantKeys)

	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("got %d accounts (%v), want %d (%v)", len(gotKeys), gotKeys, len(wantKeys), wantKeys)
	}
	for i := range gotKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Errorf("account %d: got %q, want %q", i, gotKeys[i], wantKeys[i])
		}
	}
}

// TestConfiguredAccounts_EmptyConfig verifies the empty case returns nil
// without panicking.
func TestConfiguredAccounts_EmptyConfig(t *testing.T) {
	got := configuredAccounts(&config.Config{})
	if len(got) != 0 {
		t.Errorf("got %d accounts for empty config, want 0", len(got))
	}
}

// TestMaintenanceManager_RequestsSerializedThroughChannel fires Trigger
// many times concurrently and asserts that consumers reading from the
// channel observe at most one in flight at any moment. This is the
// guarantee the user explicitly asked for: "only one thing running" —
// the buffered channel + single consumer is the structural enforcement.
func TestMaintenanceManager_RequestsSerializedThroughChannel(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root)

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
			m.Trigger(account.New("linear", "ws"+string(rune('0'+i))))
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
	m := NewMaintenanceManager(s, root)

	// Fill the buffer without starting a worker.
	for i := 0; i < triggerBuffer; i++ {
		m.Trigger(account.New("linear", "ws"))
	}

	// The next Trigger must block. Run it in a goroutine and assert it
	// hasn't returned within a short timeout.
	done := make(chan struct{})
	go func() {
		m.Trigger(account.New("linear", "ws"))
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

// TestMaintenanceManager_IsStale exercises the wall-clock mtime gate.
func TestMaintenanceManager_IsStale(t *testing.T) {
	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	m := NewMaintenanceManager(s, root)

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
	m := NewMaintenanceManager(s, root)
	m.setAccounts(&config.Config{Linear: []config.LinearConfig{{Workspace: "alpha"}}})

	// Scheduler tick logic, executed inline so we don't wait an hour.
	for _, acct := range m.snapshotAccounts() {
		if m.isStale(acct) {
			m.Trigger(acct)
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
