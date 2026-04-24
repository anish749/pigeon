package hub

import (
	"sync"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/account"
)

func TestBroadcast_FilterMatch(t *testing.T) {
	slackAcme := account.New("slack", "acme")
	slackOther := account.New("slack", "other")
	whatsapp := account.New("whatsapp", "+14155551234")

	tests := []struct {
		name    string
		filter  Filter
		event   Event
		wantHit bool
	}{
		{
			name:    "empty filter matches any account",
			filter:  Filter{},
			event:   Event{Acct: slackAcme},
			wantHit: true,
		},
		{
			name:    "single-account filter matches that account",
			filter:  Filter{Accounts: []account.Account{slackAcme}},
			event:   Event{Acct: slackAcme},
			wantHit: true,
		},
		{
			name:    "single-account filter rejects other account",
			filter:  Filter{Accounts: []account.Account{slackAcme}},
			event:   Event{Acct: slackOther},
			wantHit: false,
		},
		{
			name:    "multi-account filter matches any listed",
			filter:  Filter{Accounts: []account.Account{slackAcme, whatsapp}},
			event:   Event{Acct: whatsapp},
			wantHit: true,
		},
		{
			name:    "multi-account filter rejects unlisted",
			filter:  Filter{Accounts: []account.Account{slackAcme, whatsapp}},
			event:   Event{Acct: slackOther},
			wantHit: false,
		},
		{
			name:    "platform difference alone matters",
			filter:  Filter{Accounts: []account.Account{slackAcme}},
			event:   Event{Acct: account.New("whatsapp", "acme")},
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroadcast()
			ch, cancel := b.Subscribe(tt.filter, 4)
			defer cancel()

			b.Publish(tt.event)

			select {
			case got := <-ch:
				if !tt.wantHit {
					t.Fatalf("expected no event, got %+v", got)
				}
				if got.Acct != tt.event.Acct {
					t.Errorf("account mismatch: got %v, want %v", got.Acct, tt.event.Acct)
				}
			case <-time.After(50 * time.Millisecond):
				if tt.wantHit {
					t.Fatalf("expected event, got none")
				}
			}
		})
	}
}

func TestBroadcast_DropsWhenBufferFull(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		publish int
		wantMin int // minimum number received; extras beyond buffer may be dropped
		wantMax int // maximum number received; must not exceed bufSize when we don't read
	}{
		{
			name:    "under buffer: all delivered",
			bufSize: 4,
			publish: 3,
			wantMin: 3,
			wantMax: 3,
		},
		{
			name:    "at buffer: all delivered",
			bufSize: 4,
			publish: 4,
			wantMin: 4,
			wantMax: 4,
		},
		{
			name:    "over buffer: excess dropped, buffer full",
			bufSize: 4,
			publish: 10,
			wantMin: 4,
			wantMax: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroadcast()
			ch, cancel := b.Subscribe(Filter{}, tt.bufSize)
			defer cancel()

			// Publish without reading — exercises the drop path.
			for i := 0; i < tt.publish; i++ {
				b.Publish(Event{Kind: EventMessage})
			}

			// Now drain everything that made it in.
			received := 0
			for {
				select {
				case <-ch:
					received++
				case <-time.After(20 * time.Millisecond):
					if received < tt.wantMin || received > tt.wantMax {
						t.Errorf("received %d events, want %d..%d", received, tt.wantMin, tt.wantMax)
					}
					return
				}
			}
		})
	}
}

func TestBroadcast_CancelIdempotent(t *testing.T) {
	b := NewBroadcast()
	_, cancel := b.Subscribe(Filter{}, 4)

	// Call multiple times — must not panic or double-close.
	cancel()
	cancel()
	cancel()
}

func TestBroadcast_CancelClosesChannel(t *testing.T) {
	b := NewBroadcast()
	ch, cancel := b.Subscribe(Filter{}, 4)

	cancel()

	// Reading from a closed channel returns immediately.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected closed channel, got open")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("read from cancelled channel blocked")
	}
}

func TestBroadcast_PublishAfterCancelDoesNotPanic(t *testing.T) {
	b := NewBroadcast()
	_, cancel := b.Subscribe(Filter{}, 4)
	cancel()

	// Publish to a broadcast whose only subscriber has cancelled must
	// not panic and must not block.
	done := make(chan struct{})
	go func() {
		b.Publish(Event{Kind: EventMessage})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Publish blocked after subscriber cancel")
	}
}

func TestBroadcast_MultipleSubscribersIndependent(t *testing.T) {
	b := NewBroadcast()

	slackAcme := account.New("slack", "acme")
	whatsapp := account.New("whatsapp", "+14155551234")

	tests := []struct {
		name        string
		subAFilter  Filter
		subBFilter  Filter
		event       Event
		wantAReceive bool
		wantBReceive bool
	}{
		{
			name:        "both unfiltered both receive",
			subAFilter:  Filter{},
			subBFilter:  Filter{},
			event:       Event{Acct: slackAcme},
			wantAReceive: true,
			wantBReceive: true,
		},
		{
			name:        "disjoint filters only matching receives",
			subAFilter:  Filter{Accounts: []account.Account{slackAcme}},
			subBFilter:  Filter{Accounts: []account.Account{whatsapp}},
			event:       Event{Acct: slackAcme},
			wantAReceive: true,
			wantBReceive: false,
		},
		{
			name:        "overlapping filters both receive",
			subAFilter:  Filter{Accounts: []account.Account{slackAcme}},
			subBFilter:  Filter{Accounts: []account.Account{slackAcme, whatsapp}},
			event:       Event{Acct: slackAcme},
			wantAReceive: true,
			wantBReceive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chA, cancelA := b.Subscribe(tt.subAFilter, 4)
			chB, cancelB := b.Subscribe(tt.subBFilter, 4)
			defer cancelA()
			defer cancelB()

			b.Publish(tt.event)

			aGot := drainOne(chA, 50*time.Millisecond)
			bGot := drainOne(chB, 50*time.Millisecond)

			if aGot != tt.wantAReceive {
				t.Errorf("sub A receive: got %v, want %v", aGot, tt.wantAReceive)
			}
			if bGot != tt.wantBReceive {
				t.Errorf("sub B receive: got %v, want %v", bGot, tt.wantBReceive)
			}
		})
	}
}

func TestBroadcast_ConcurrentPublishersAndSubscribers(t *testing.T) {
	// Smoke test for races; run under `go test -race`.
	b := NewBroadcast()

	const (
		publishers    = 8
		perPublisher  = 100
		subscribers   = 4
	)

	// Start subscribers first so they see all publishes.
	var wgSubs sync.WaitGroup
	received := make([]int, subscribers)
	for i := 0; i < subscribers; i++ {
		ch, cancel := b.Subscribe(Filter{}, 1024)
		defer cancel()
		wgSubs.Add(1)
		go func(idx int, ch <-chan Event) {
			defer wgSubs.Done()
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					received[idx]++
				case <-time.After(200 * time.Millisecond):
					return
				}
			}
		}(i, ch)
	}

	var wgPubs sync.WaitGroup
	for i := 0; i < publishers; i++ {
		wgPubs.Add(1)
		go func() {
			defer wgPubs.Done()
			for j := 0; j < perPublisher; j++ {
				b.Publish(Event{Kind: EventMessage})
			}
		}()
	}
	wgPubs.Wait()
	wgSubs.Wait()

	// Each subscriber should have received approximately publishers*perPublisher;
	// some drops are acceptable since bufSize is 1024 and we publish 800.
	for i, n := range received {
		if n == 0 {
			t.Errorf("subscriber %d received 0 events", i)
		}
	}
}

func drainOne(ch <-chan Event, timeout time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}
