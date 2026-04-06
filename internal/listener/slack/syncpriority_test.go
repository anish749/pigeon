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
	toSync := prioritizeChannels(context.Background(), &fakeSearcher{}, noopGate(), nil, makeChannels(100))
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels, got %d", len(toSync))
	}
}

func TestPrioritize_FewChannels_ReturnsAll(t *testing.T) {
	toSync := prioritizeChannels(context.Background(), &fakeSearcher{}, noopGate(), cursorAt(time.Now()), makeChannels(30))
	if len(toSync) != 30 {
		t.Fatalf("expected 30 channels, got %d", len(toSync))
	}
}

func TestPrioritize_ZeroResults_ReturnsNone(t *testing.T) {
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 0},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 0 {
		t.Fatalf("expected 0 channels, got %d", len(toSync))
	}
}

func TestPrioritize_SearchError_ReturnsAll(t *testing.T) {
	searcher := &fakeSearcher{err: fmt.Errorf("missing_scope")}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels on error, got %d", len(toSync))
	}
}

func TestPrioritize_SmallActiveSet(t *testing.T) {
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 3, Matches: makeMatches("C0001", "C0005", "C0010")},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(toSync))
	}
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
	ids := make([]string, 70)
	for i := range ids {
		ids[i] = fmt.Sprintf("C%04d", i)
	}
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 70, Matches: makeMatches(ids...)},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels when threshold exceeded, got %d", len(toSync))
	}
}

func TestPrioritize_ExceedsThresholdOnLaterPage_ReturnsAll(t *testing.T) {
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
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels when threshold exceeded on page 2, got %d", len(toSync))
	}
}

func TestPrioritize_DuplicatePageStillPaginates(t *testing.T) {
	// Page 1 and 2 have the same channels, but page 3 adds a new one.
	// All pages should be visited — duplicates on a page don't mean we're done.
	matches := makeMatches("C0001", "C0002", "C0003", "C0004", "C0005")
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 300, Matches: matches},
			2: {Total: 300, Matches: matches},
			3: {Total: 300, Matches: makeMatches("C0001", "C0099")},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 6 {
		t.Fatalf("expected 6 channels, got %d", len(toSync))
	}
}

func TestPrioritize_MultiPageAccumulation(t *testing.T) {
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 300, Matches: makeMatches("C0001", "C0002")},
			2: {Total: 300, Matches: makeMatches("C0003", "C0004")},
			3: {Total: 300, Matches: makeMatches("C0001", "C0003")},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 4 {
		t.Fatalf("expected 4 channels, got %d", len(toSync))
	}
}

func TestPrioritize_PageErrorSyncsAll(t *testing.T) {
	searcher := &failingPageSearcher{
		base: &fakeSearcher{
			pages: map[int]*goslack.SearchMessages{
				1: {Total: 200, Matches: makeMatches("C0001", "C0002")},
			},
		},
		failPage: 2,
		failErr:  fmt.Errorf("timeout"),
	}
	// Mid-pagination failure falls back to syncing all channels rather than
	// trusting a partial set that may be missing active channels.
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), makeChannels(100))
	if len(toSync) != 100 {
		t.Fatalf("expected 100 channels on page error, got %d", len(toSync))
	}
}

func TestPrioritize_ResultIsSorted(t *testing.T) {
	channels := []goslack.Channel{
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0000"}}},            // public
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0001", IsIM: true}}}, // DM
		{GroupConversation: goslack.GroupConversation{Conversation: goslack.Conversation{ID: "C0002", IsMpIM: true}}}, // mpim
	}
	searcher := &fakeSearcher{
		pages: map[int]*goslack.SearchMessages{
			1: {Total: 3, Matches: makeMatches("C0000", "C0001", "C0002")},
		},
	}
	toSync := prioritizeChannels(context.Background(), searcher, noopGate(), cursorAt(time.Now()), channels)

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
