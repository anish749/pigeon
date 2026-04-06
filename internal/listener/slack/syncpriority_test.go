package slack

import (
	"context"
	"fmt"
	"testing"
	"time"

	goslack "github.com/slack-go/slack"
)

// fakeSearcher implements messageSearcher for tests.
type fakeSearcher struct {
	pages map[int]*goslack.SearchMessages
	err   error
}

func (f *fakeSearcher) SearchMessagesContext(_ context.Context, _ string, params goslack.SearchParameters) (*goslack.SearchMessages, error) {
	if f.err != nil {
		return nil, f.err
	}
	if resp, ok := f.pages[params.Page]; ok {
		return resp, nil
	}
	return &goslack.SearchMessages{}, nil
}

type failingPageSearcher struct {
	base     *fakeSearcher
	failPage int
	failErr  error
}

func (f *failingPageSearcher) SearchMessagesContext(ctx context.Context, query string, params goslack.SearchParameters) (*goslack.SearchMessages, error) {
	if params.Page == f.failPage {
		return nil, f.failErr
	}
	return f.base.SearchMessagesContext(ctx, query, params)
}

func makeChannels(n int) []goslack.Channel {
	chs := make([]goslack.Channel, n)
	for i := range chs {
		chs[i].ID = fmt.Sprintf("C%04d", i)
	}
	return chs
}

func makeMatches(channelIDs ...string) []goslack.SearchMessage {
	var matches []goslack.SearchMessage
	for _, id := range channelIDs {
		matches = append(matches, goslack.SearchMessage{
			Channel: goslack.CtxChannel{ID: id},
		})
	}
	return matches
}

func cursorAt(t time.Time) syncCursors {
	return syncCursors{"C0000": fmt.Sprintf("%d.000000", t.Unix())}
}

func noopGate() *rateLimitGate {
	return &rateLimitGate{}
}

// --- prioritizeChannels tests ---

func TestPrioritize_NoCursors_ReturnsAll(t *testing.T) {
	channels := makeChannels(100)
	toSync, skipped, _ := prioritizeChannels(context.Background(), &fakeSearcher{}, noopGate(), nil, channels)
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels, got %d", len(toSync))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
}

func TestPrioritize_FewChannels_ReturnsAll(t *testing.T) {
	channels := makeChannels(30)
	cursors := cursorAt(time.Now())
	toSync, skipped, _ := prioritizeChannels(context.Background(), &fakeSearcher{}, noopGate(), cursors, channels)
	if len(toSync) != 30 {
		t.Fatalf("expected 30 channels, got %d", len(toSync))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
}

func TestPrioritize_ZeroResults_ReturnsNone(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 0},
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 0 {
		t.Fatalf("expected 0 channels, got %d", len(toSync))
	}
	if skipped != 100 {
		t.Fatalf("expected 100 skipped, got %d", skipped)
	}
}

func TestPrioritize_SearchError_ReturnsAll(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{err: fmt.Errorf("missing_scope")}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels on error, got %d", len(toSync))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped on error, got %d", skipped)
	}
}

func TestPrioritize_SmallActiveSet(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 3, Matches: makeMatches("C0001", "C0005", "C0010")},
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(toSync))
	}
	if skipped != 97 {
		t.Fatalf("expected 97 skipped, got %d", skipped)
	}
	// Verify the right channels are in the list.
	ids := map[string]bool{}
	for _, ch := range toSync {
		ids[ch.ID] = true
	}
	for _, want := range []string{"C0001", "C0005", "C0010"} {
		if !ids[want] {
			t.Fatalf("expected %s in toSync", want)
		}
	}
}

func TestPrioritize_ExceedsThresholdPage1_ReturnsAll(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())

	// 70 unique channels out of 100 → 70% > 65% threshold.
	ids := make([]string, 70)
	for i := range ids {
		ids[i] = fmt.Sprintf("C%04d", i)
	}
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 70, Matches: makeMatches(ids...)},
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels when threshold exceeded, got %d", len(toSync))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped when threshold exceeded, got %d", skipped)
	}
}

func TestPrioritize_ExceedsThresholdOnLaterPage_ReturnsAll(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())

	page1IDs := make([]string, 30)
	for i := range page1IDs {
		page1IDs[i] = fmt.Sprintf("C%04d", i)
	}
	page2IDs := make([]string, 40)
	for i := range page2IDs {
		page2IDs[i] = fmt.Sprintf("C%04d", 30+i)
	}
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 200, Matches: makeMatches(page1IDs...)},
			2: {Total: 200, Matches: makeMatches(page2IDs...)},
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels when threshold exceeded on page 2, got %d", len(toSync))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
}

func TestPrioritize_StopsOnPlateau(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())

	matches := makeMatches("C0001", "C0002", "C0003", "C0004", "C0005")
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 200, Matches: matches},
			2: {Total: 200, Matches: matches}, // all duplicates
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 5 {
		t.Fatalf("expected 5 channels after plateau, got %d", len(toSync))
	}
	if skipped != 95 {
		t.Fatalf("expected 95 skipped, got %d", skipped)
	}
}

func TestPrioritize_MultiPageAccumulation(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())

	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 300, Matches: makeMatches("C0001", "C0002")},
			2: {Total: 300, Matches: makeMatches("C0003", "C0004")},
			3: {Total: 300, Matches: makeMatches("C0001", "C0003")}, // all seen → plateau
		},
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 4 {
		t.Fatalf("expected 4 channels, got %d", len(toSync))
	}
	if skipped != 96 {
		t.Fatalf("expected 96 skipped, got %d", skipped)
	}
}

func TestPrioritize_PageErrorUsesPartialResults(t *testing.T) {
	channels := makeChannels(100)
	cursors := cursorAt(time.Now())

	searcher := &failingPageSearcher{
		base: &fakeSearcher{
			pages: map[int]*goslack.SearchMessages{
				1: {Total: 200, Matches: makeMatches("C0001", "C0002")},
			},
		},
		failPage: 2,
		failErr:  fmt.Errorf("timeout"),
	}
	toSync, skipped, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)
	if len(toSync) != 2 {
		t.Fatalf("expected 2 channels from page 1, got %d", len(toSync))
	}
	if skipped != 98 {
		t.Fatalf("expected 98 skipped, got %d", skipped)
	}
}

func TestPrioritize_ResultIsSorted(t *testing.T) {
	// Create channels of mixed types: public (C0000), DM (C0001), mpim (C0002).
	channels := []goslack.Channel{
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0000"}}},                                        // public
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0001", IsIM: true}}},                             // DM
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0002", IsMpIM: true}}}, // mpim
	}
	// All three active.
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 3, Matches: makeMatches("C0000", "C0001", "C0002")},
		},
	}
	toSync, _, _ := prioritizeChannels(context.Background(), searcher, noopGate(), cursors, channels)

	// Should be sorted: DM first, then mpim, then public.
	if len(toSync) != 3 {
		t.Fatalf("expected 3, got %d", len(toSync))
	}
	if toSync[0].ID != "C0001" {
		t.Fatalf("expected DM (C0001) first, got %s", toSync[0].ID)
	}
	if toSync[1].ID != "C0002" {
		t.Fatalf("expected mpim (C0002) second, got %s", toSync[1].ID)
	}
	if toSync[2].ID != "C0000" {
		t.Fatalf("expected public (C0000) third, got %s", toSync[2].ID)
	}
}

// --- Unit tests for helper functions ---

func TestMaxCursorTime(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	cursors := syncCursors{
		"C0001": fmt.Sprintf("%d.000000", old.Unix()),
		"C0002": fmt.Sprintf("%d.000000", now.Unix()),
		"C0003": fmt.Sprintf("%d.000000", old.Unix()),
	}
	got := maxCursorTime(cursors)
	if got.Unix() != now.Unix() {
		t.Fatalf("expected max cursor %d, got %d", now.Unix(), got.Unix())
	}
}

func TestMaxCursorTime_Empty(t *testing.T) {
	got := maxCursorTime(syncCursors{})
	if !got.IsZero() {
		t.Fatal("expected zero time for empty cursors")
	}
}

func TestCollectChannelIDs(t *testing.T) {
	matches := makeMatches("C001", "C002", "C001", "C003", "")
	got := collectChannelIDs(matches)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique IDs, got %d", len(got))
	}
	for _, id := range []string{"C001", "C002", "C003"} {
		if !got[id] {
			t.Fatalf("missing %s", id)
		}
	}
}
