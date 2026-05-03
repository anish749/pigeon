package syncstatus

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStartAndDone(t *testing.T) {
	tr := NewTracker()

	tr.Start("slack/test", KindBackfill)
	all := tr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
	info := all["slack/test"]
	if !info.Syncing {
		t.Error("expected syncing=true after Start")
	}
	if info.Kind != KindBackfill {
		t.Errorf("expected kind %q, got %q", KindBackfill, info.Kind)
	}
	if info.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
	if info.CompletedAt != nil {
		t.Error("expected CompletedAt to be nil before Done")
	}

	tr.Done("slack/test", nil)
	info = tr.All()["slack/test"]
	if info.Syncing {
		t.Error("expected syncing=false after Done")
	}
	if info.CompletedAt == nil {
		t.Error("expected CompletedAt to be set after Done")
	}
	if info.Error != "" {
		t.Errorf("expected no error, got %q", info.Error)
	}
}

func TestDoneWithError(t *testing.T) {
	tr := NewTracker()
	tr.Start("wa/phone", KindHistory)
	tr.Done("wa/phone", errors.New("connection lost"))

	info := tr.All()["wa/phone"]
	if info.Error != "connection lost" {
		t.Errorf("expected error %q, got %q", "connection lost", info.Error)
	}
}

func TestUpdate(t *testing.T) {
	tr := NewTracker()
	tr.Start("slack/ws", KindBackfill)
	tr.Update("slack/ws", "dms: 3/10")

	info := tr.All()["slack/ws"]
	if info.Detail != "dms: 3/10" {
		t.Errorf("expected detail %q, got %q", "dms: 3/10", info.Detail)
	}

	// Done clears detail.
	tr.Done("slack/ws", nil)
	info = tr.All()["slack/ws"]
	if info.Detail != "" {
		t.Errorf("expected empty detail after Done, got %q", info.Detail)
	}
}

func TestUpdateNoOp(t *testing.T) {
	tr := NewTracker()
	// Update on unknown key should not panic or create an entry.
	tr.Update("nonexistent", "detail")
	if len(tr.All()) != 0 {
		t.Error("Update on unknown key should not create an entry")
	}
}

func TestDoneWithoutStart(t *testing.T) {
	tr := NewTracker()
	tr.Done("new/key", nil)

	info := tr.All()["new/key"]
	if info.Syncing {
		t.Error("expected syncing=false")
	}
	if info.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestMultipleAccounts(t *testing.T) {
	tr := NewTracker()
	tr.Start("slack/a", KindBackfill)
	tr.Start("gws/b", KindPoll)
	tr.Done("slack/a", nil)

	all := tr.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all["slack/a"].Syncing {
		t.Error("slack/a should be idle")
	}
	if !all["gws/b"].Syncing {
		t.Error("gws/b should be syncing")
	}
}

func TestAllReturnsSnapshot(t *testing.T) {
	tr := NewTracker()
	tr.Start("key", KindPoll)

	snap := tr.All()
	tr.Done("key", nil)

	// Snapshot should still show syncing=true.
	if !snap["key"].Syncing {
		t.Error("snapshot should not be affected by later mutations")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tr := NewTracker()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key"
			tr.Start(key, KindPoll)
			tr.Update(key, "progress")
			tr.All()
			tr.Done(key, nil)
		}(i)
	}

	wg.Wait()

	// Should not panic; final state is idle.
	info := tr.All()["key"]
	if info.Syncing {
		t.Error("expected idle after all goroutines done")
	}
}

func TestStartResetsError(t *testing.T) {
	tr := NewTracker()
	tr.Start("key", KindPoll)
	tr.Done("key", errors.New("first error"))

	tr.Start("key", KindPoll)
	info := tr.All()["key"]
	if info.Error != "" {
		t.Errorf("Start should clear previous error, got %q", info.Error)
	}
}

func TestTimestampOrdering(t *testing.T) {
	tr := NewTracker()
	tr.Start("key", KindPoll)
	time.Sleep(time.Millisecond)
	tr.Done("key", nil)

	info := tr.All()["key"]
	if !info.CompletedAt.After(*info.StartedAt) {
		t.Error("CompletedAt should be after StartedAt")
	}
}

func TestIsSyncing(t *testing.T) {
	tr := NewTracker()
	if tr.IsSyncing("unknown") {
		t.Error("IsSyncing(unknown) = true, want false")
	}
	tr.Start("key", KindPoll)
	if !tr.IsSyncing("key") {
		t.Error("IsSyncing during sync = false, want true")
	}
	tr.Done("key", nil)
	if tr.IsSyncing("key") {
		t.Error("IsSyncing after Done = true, want false")
	}
}

func TestWaitForDone_NotSyncing(t *testing.T) {
	tr := NewTracker()
	if ch := tr.WaitForDone("unknown"); ch != nil {
		t.Error("WaitForDone(unknown) returned non-nil channel, want nil")
	}
	tr.Start("key", KindPoll)
	tr.Done("key", nil)
	if ch := tr.WaitForDone("key"); ch != nil {
		t.Error("WaitForDone(after Done) returned non-nil channel, want nil")
	}
}

func TestWaitForDone_ClosesOnDone(t *testing.T) {
	tr := NewTracker()
	tr.Start("key", KindPoll)
	ch := tr.WaitForDone("key")
	if ch == nil {
		t.Fatal("WaitForDone during sync returned nil, want non-nil channel")
	}
	select {
	case <-ch:
		t.Fatal("channel closed before Done was called")
	case <-time.After(20 * time.Millisecond):
		// expected: still open
	}

	tr.Done("key", nil)
	select {
	case <-ch:
		// expected: closed
	case <-time.After(time.Second):
		t.Fatal("channel did not close after Done")
	}
}

// TestWaitForDone_StartAfterStartClosesPriorChannel covers the defensive
// branch where Start is called twice without an intervening Done: any
// subscriber from the first sync should be released at the boundary
// rather than waiting forever for a Done that's been overwritten.
func TestWaitForDone_StartAfterStartClosesPriorChannel(t *testing.T) {
	tr := NewTracker()
	tr.Start("key", KindPoll)
	ch := tr.WaitForDone("key")
	if ch == nil {
		t.Fatal("WaitForDone during sync returned nil")
	}
	tr.Start("key", KindPoll) // double Start without Done
	select {
	case <-ch:
		// expected: prior channel closed at the Start boundary
	case <-time.After(time.Second):
		t.Fatal("prior channel did not close after second Start")
	}
}
