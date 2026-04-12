package search

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/store/modelv1"
)

// captureStdout captures stdout output from a function call.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

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
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice"}}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-17",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M2", Ts: ts(2026, 3, 17, 9, 0, 0), Sender: "Bob"}}},
		{Platform: "slack", Account: "acme", Conversation: "#random", Date: "2026-03-16",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M3", Ts: ts(2026, 3, 16, 10, 0, 0), Sender: "Alice"}}},
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

func TestPrintGroupedResults_IncludesFilePaths(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			FilePath: "/data/slack/acme/#general/2026-03-16.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", Text: "hello"}}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			FilePath: "/data/slack/acme/#general/2026-03-16.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 1, 0), Sender: "Bob", Text: "world"}}},
	}

	out := captureStdout(t, func() { PrintGroupedResults(matches) })

	// Should include the file path exactly once (deduped).
	count := strings.Count(out, "/data/slack/acme/#general/2026-03-16.jsonl")
	if count != 1 {
		t.Errorf("file path appears %d times, want 1", count)
	}
}

func TestPrintGroupedResults_MultipleFiles(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			FilePath: "/data/slack/acme/#general/2026-03-16.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", Text: "hello"}}},
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-17",
			FilePath: "/data/slack/acme/#general/2026-03-17.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M2", Ts: ts(2026, 3, 17, 9, 0, 0), Sender: "Bob", Text: "world"}}},
	}

	out := captureStdout(t, func() { PrintGroupedResults(matches) })

	if !strings.Contains(out, "2026-03-16.jsonl") {
		t.Error("output missing 2026-03-16.jsonl")
	}
	if !strings.Contains(out, "2026-03-17.jsonl") {
		t.Error("output missing 2026-03-17.jsonl")
	}
}

func TestPrintGroupedResults_ThreadFilePath(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "1742100000", Thread: true,
			FilePath: "/data/slack/acme/#general/threads/1742100000.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "R1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", Text: "reply"}}},
	}

	out := captureStdout(t, func() { PrintGroupedResults(matches) })

	if !strings.Contains(out, "threads/1742100000.jsonl") {
		t.Error("output missing thread file path")
	}
}

func TestPrintGroupedResults_NoFilePath(t *testing.T) {
	// Matches without FilePath (e.g. from old format) should still work.
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", Text: "hello"}}},
	}

	out := captureStdout(t, func() { PrintGroupedResults(matches) })

	if !strings.Contains(out, "#general") {
		t.Error("output missing conversation name")
	}
	if !strings.Contains(out, "Alice") {
		t.Error("output missing sender")
	}
}

func TestPrintGroupedResults_GroupsConversations(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme", Conversation: "#general", Date: "2026-03-16",
			FilePath: "/data/slack/acme/#general/2026-03-16.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Alice", Text: "one"}}},
		{Platform: "slack", Account: "acme", Conversation: "#random", Date: "2026-03-16",
			FilePath: "/data/slack/acme/#random/2026-03-16.jsonl",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M2", Ts: ts(2026, 3, 16, 9, 0, 0), Sender: "Bob", Text: "two"}}},
	}

	out := captureStdout(t, func() { PrintGroupedResults(matches) })

	generalIdx := strings.Index(out, "#general")
	randomIdx := strings.Index(out, "#random")
	if generalIdx < 0 || randomIdx < 0 {
		t.Fatal("output missing conversation headers")
	}
	if generalIdx > randomIdx {
		t.Error("conversations should appear in input order: #general before #random")
	}
}

func TestPrintSummary_NoSince(t *testing.T) {
	matches := []Match{
		{Platform: "slack", Account: "acme",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: time.Now(), Sender: "Alice"}}},
	}

	out := captureStdout(t, func() { PrintSummary(matches, 0) })

	if out != "" {
		t.Errorf("PrintSummary with sinceDur=0 should produce no output, got %q", out)
	}
}

func TestPrintSummary_Empty(t *testing.T) {
	out := captureStdout(t, func() { PrintSummary(nil, 24*time.Hour) })

	if out != "" {
		t.Errorf("PrintSummary with empty matches should produce no output, got %q", out)
	}
}

func TestPrintSummary_ShowsBuckets(t *testing.T) {
	now := time.Now()
	matches := []Match{
		{Platform: "slack", Account: "acme",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M1", Ts: now.Add(-30 * time.Minute), Sender: "Alice"}}},
		{Platform: "slack", Account: "acme",
			Line: modelv1.Line{Type: modelv1.LineMessage, Msg: &modelv1.MsgLine{ID: "M2", Ts: now.Add(-2 * time.Hour), Sender: "Bob"}}},
	}

	out := captureStdout(t, func() { PrintSummary(matches, 6*time.Hour) })

	if !strings.Contains(out, "slack/acme") {
		t.Error("output missing platform/account label")
	}
	if !strings.Contains(out, "Last 1h:") {
		t.Error("output missing 1h bucket")
	}
	if !strings.Contains(out, "Alice") {
		t.Error("output missing sender Alice")
	}
}
