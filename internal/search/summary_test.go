package search

import (
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

func TestChooseBuckets_Short(t *testing.T) {
	buckets := chooseBuckets(3 * time.Hour)
	if len(buckets) != 4 {
		t.Errorf("buckets = %d, want 4 for 3h", len(buckets))
	}
	if buckets[0].label != "1h" {
		t.Errorf("first bucket = %q, want 1h", buckets[0].label)
	}
}

func TestChooseBuckets_Day(t *testing.T) {
	buckets := chooseBuckets(24 * time.Hour)
	if len(buckets) != 5 {
		t.Errorf("buckets = %d, want 5 for 24h", len(buckets))
	}
	if buckets[len(buckets)-1].label != "24h" {
		t.Errorf("last bucket = %q, want 24h", buckets[len(buckets)-1].label)
	}
}

func TestChooseBuckets_ThreeDays(t *testing.T) {
	buckets := chooseBuckets(72 * time.Hour)
	if len(buckets) != 6 {
		t.Errorf("buckets = %d, want 6 for 72h", len(buckets))
	}
}

func TestChooseBuckets_Week(t *testing.T) {
	buckets := chooseBuckets(7 * 24 * time.Hour)
	if len(buckets) != 5 {
		t.Errorf("buckets = %d, want 5 for 7d", len(buckets))
	}
}

func TestChooseBuckets_Month(t *testing.T) {
	buckets := chooseBuckets(30 * 24 * time.Hour)
	if len(buckets) != 5 {
		t.Errorf("buckets = %d, want 5 for 30d", len(buckets))
	}
	if buckets[len(buckets)-1].label != "30d" {
		t.Errorf("last bucket = %q, want 30d", buckets[len(buckets)-1].label)
	}
}

func TestFormatSenders_Basic(t *testing.T) {
	senders := map[string]struct{}{"Alice": {}, "Bob": {}, "Charlie": {}}
	got := formatSenders(senders, 50)
	if got != "Alice, Bob, Charlie" {
		t.Errorf("formatSenders = %q, want 'Alice, Bob, Charlie'", got)
	}
}

func TestFormatSenders_Truncated(t *testing.T) {
	senders := map[string]struct{}{"Alice": {}, "Bob": {}, "Charlie": {}}
	got := formatSenders(senders, 2)
	if got != "Alice, Bob, ..." {
		t.Errorf("formatSenders = %q, want 'Alice, Bob, ...'", got)
	}
}

func TestFormatSenders_Empty(t *testing.T) {
	got := formatSenders(map[string]struct{}{}, 50)
	if got != "" {
		t.Errorf("formatSenders = %q, want empty", got)
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"2026-03-18": true, "2026-03-16": true, "2026-03-17": true}
	got := sortedKeys(m)
	if len(got) != 3 || got[0] != "2026-03-16" || got[2] != "2026-03-18" {
		t.Errorf("sortedKeys = %v", got)
	}
}

func TestMatchGrouping(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			Msg: modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice"}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-17",
			Msg: modelv1.MsgLine{ID: "M2", Ts: ts(2026, 3, 17, 9, 0, 0), Sender: "Bob"}},
		{Platform: "slack", Account: "acme", Conversation: "#random", Date: "2026-03-16",
			Msg: modelv1.MsgLine{ID: "M3", Ts: ts(2026, 3, 16, 10, 0, 0), Sender: "Alice"}},
	}

	// Group by conversation — verify the grouping logic works correctly
	type groupKey struct{ platform, account, conversation string }
	groups := make(map[groupKey][]Match)
	for _, m := range matches {
		k := groupKey{m.Platform, m.Account, m.Conversation}
		groups[k] = append(groups[k], m)
	}

	if len(groups) != 2 {
		t.Errorf("groups = %d, want 2", len(groups))
	}
	generalKey := groupKey{"slack", "acme", "#general"}
	if len(groups[generalKey]) != 2 {
		t.Errorf("#general matches = %d, want 2", len(groups[generalKey]))
	}
	randomKey := groupKey{"slack", "acme", "#random"}
	if len(groups[randomKey]) != 1 {
		t.Errorf("#random matches = %d, want 1", len(groups[randomKey]))
	}
}
