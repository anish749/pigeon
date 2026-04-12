package syncstatus

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStartAndDone(t *testing.T) {
	tr := NewTracker()

	tr.Start("slack/test")
	all := tr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
	info := all["slack/test"]
	if !info.Syncing {
		t.Error("expected syncing=true after Start")
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
	tr.Start("wa/phone")
	tr.Done("wa/phone", errors.New("connection lost"))

	info := tr.All()["wa/phone"]
	if info.Error != "connection lost" {
		t.Errorf("expected error %q, got %q", "connection lost", info.Error)
	}
}

func TestUpdate(t *testing.T) {
	tr := NewTracker()
	tr.Start("slack/ws")
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
	tr.Start("slack/a")
	tr.Start("gws/b")
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
	tr.Start("key")

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
			tr.Start(key)
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
	tr.Start("key")
	tr.Done("key", errors.New("first error"))

	tr.Start("key")
	info := tr.All()["key"]
	if info.Error != "" {
		t.Errorf("Start should clear previous error, got %q", info.Error)
	}
}

func TestTimestampOrdering(t *testing.T) {
	tr := NewTracker()
	tr.Start("key")
	time.Sleep(time.Millisecond)
	tr.Done("key", nil)

	info := tr.All()["key"]
	if !info.CompletedAt.After(*info.StartedAt) {
		t.Error("CompletedAt should be after StartedAt")
	}
}
