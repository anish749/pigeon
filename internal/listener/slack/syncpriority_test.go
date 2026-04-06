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
	// pages maps page number to the response for that page.
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

func TestSyncPriority_NoCursors(t *testing.T) {
	p := computeSyncPriority(context.Background(), &fakeSearcher{}, noopGate(), nil, makeChannels(100))
	if p.activeChannels != nil {
		t.Fatal("expected nil activeChannels (sync all) for no cursors")
	}
	if !p.shouldSync("anything") {
		t.Fatal("shouldSync must return true when activeChannels is nil")
	}
}

func TestSyncPriority_FewChannels(t *testing.T) {
	cursors := cursorAt(time.Now())
	p := computeSyncPriority(context.Background(), &fakeSearcher{}, noopGate(), cursors, makeChannels(30))
	if p.activeChannels != nil {
		t.Fatal("expected nil activeChannels (sync all) for few channels")
	}
}

func TestSyncPriority_SearchReturnsZero(t *testing.T) {
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 0},
		},
	}
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, makeChannels(100))
	if p.activeChannels == nil {
		t.Fatal("expected non-nil activeChannels for zero results")
	}
	if len(p.activeChannels) != 0 {
		t.Fatalf("expected 0 active channels, got %d", len(p.activeChannels))
	}
	if p.shouldSync("C0000") {
		t.Fatal("shouldSync must return false when no channels are active")
	}
}

func TestSyncPriority_SearchError(t *testing.T) {
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{err: fmt.Errorf("missing_scope")}
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, makeChannels(100))
	if p.activeChannels != nil {
		t.Fatal("expected nil activeChannels (sync all) on search error")
	}
}

func TestSyncPriority_SmallActiveSet(t *testing.T) {
	cursors := cursorAt(time.Now())
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {
				Total:   3,
				Matches: makeMatches("C0001", "C0005", "C0010"),
			},
		},
	}
	channels := makeChannels(100)
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, channels)

	if p.activeChannels == nil {
		t.Fatal("expected non-nil activeChannels for small active set")
	}
	if len(p.activeChannels) != 3 {
		t.Fatalf("expected 3 active channels, got %d", len(p.activeChannels))
	}
	if !p.shouldSync("C0001") {
		t.Fatal("C0001 should be in active set")
	}
	if !p.shouldSync("C0005") {
		t.Fatal("C0005 should be in active set")
	}
	if p.shouldSync("C0002") {
		t.Fatal("C0002 should not be in active set")
	}
}

func TestSyncPriority_ExceedsThresholdPage1(t *testing.T) {
	cursors := cursorAt(time.Now())

	// 70 unique channels out of 100 → 70% > 65% threshold → sync all.
	ids := make([]string, 70)
	for i := range ids {
		ids[i] = fmt.Sprintf("C%04d", i)
	}

	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {
				Total:   70,
				Matches: makeMatches(ids...),
			},
		},
	}
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, makeChannels(100))
	if p.activeChannels != nil {
		t.Fatal("expected nil activeChannels (sync all) when threshold exceeded on page 1")
	}
}

func TestSyncPriority_ExceedsThresholdOnLaterPage(t *testing.T) {
	cursors := cursorAt(time.Now())
	channels := makeChannels(100)

	// Page 1: 30 channels. Page 2: 40 new channels. Total 70 > 65% → sync all.
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
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, channels)
	if p.activeChannels != nil {
		t.Fatal("expected nil activeChannels (sync all) when threshold exceeded on page 2")
	}
}

func TestSyncPriority_StopsOnPlateau(t *testing.T) {
	cursors := cursorAt(time.Now())
	channels := makeChannels(100)

	// Page 1: 5 channels. Page 2: same 5 channels (no new). Should stop and return 5.
	matches := makeMatches("C0001", "C0002", "C0003", "C0004", "C0005")
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 200, Matches: matches},
			2: {Total: 200, Matches: matches}, // all duplicates
		},
	}
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, channels)
	if p.activeChannels == nil {
		t.Fatal("expected non-nil activeChannels after plateau")
	}
	if len(p.activeChannels) != 5 {
		t.Fatalf("expected 5 active channels, got %d", len(p.activeChannels))
	}
}

func TestSyncPriority_MultiPageAccumulation(t *testing.T) {
	cursors := cursorAt(time.Now())
	channels := makeChannels(100)

	// Page 1: C0001, C0002. Page 2: C0003, C0004. Page 3: no new → stop.
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 300, Matches: makeMatches("C0001", "C0002")},
			2: {Total: 300, Matches: makeMatches("C0003", "C0004")},
			3: {Total: 300, Matches: makeMatches("C0001", "C0003")}, // all seen
		},
	}
	p := computeSyncPriority(context.Background(), searcher, noopGate(), cursors, channels)
	if p.activeChannels == nil {
		t.Fatal("expected non-nil activeChannels")
	}
	if len(p.activeChannels) != 4 {
		t.Fatalf("expected 4 active channels, got %d", len(p.activeChannels))
	}
	for _, id := range []string{"C0001", "C0002", "C0003", "C0004"} {
		if !p.shouldSync(id) {
			t.Fatalf("%s should be in active set", id)
		}
	}
}

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

func TestShouldSync_NilActiveChannels(t *testing.T) {
	p := &syncPriority{activeChannels: nil}
	if !p.shouldSync("anything") {
		t.Fatal("nil activeChannels should sync everything")
	}
}

func TestShouldSync_EmptyActiveChannels(t *testing.T) {
	p := &syncPriority{activeChannels: map[string]bool{}}
	if p.shouldSync("C0001") {
		t.Fatal("empty activeChannels should sync nothing")
	}
}

func TestSyncPriority_SearchPageError(t *testing.T) {
	cursors := cursorAt(time.Now())
	channels := makeChannels(100)

	// Page 1 succeeds with 2 channels. Page 2 fails. Should use page 1 results.
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 200, Matches: makeMatches("C0001", "C0002")},
		},
		// Page 2 not in map → returns empty, but let's make it fail explicitly.
	}
	// Override to make page 2 fail.
	failOnPage2 := &failingPageSearcher{
		base: searcher,
		failPage: 2,
		failErr:  fmt.Errorf("timeout"),
	}
	p := computeSyncPriority(context.Background(), failOnPage2, noopGate(), cursors, channels)
	if p.activeChannels == nil {
		t.Fatal("expected non-nil activeChannels when later page fails")
	}
	if len(p.activeChannels) != 2 {
		t.Fatalf("expected 2 active channels from page 1, got %d", len(p.activeChannels))
	}
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
