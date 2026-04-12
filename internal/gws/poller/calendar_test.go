package poller_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/gws/poller"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// TestCalendarBackfillLive runs a full calendar sync lifecycle against the
// real Google Calendar API. Creates its own test events and cleans them up.
//
// Run with: GWS_LIVE_TEST=1 go test ./internal/gws/poller/ -run TestCalendarBackfillLive -v -timeout 120s
func TestCalendarBackfillLive(t *testing.T) {
	if os.Getenv("GWS_LIVE_TEST") == "" {
		t.Skip("set GWS_LIVE_TEST=1 to run live calendar test")
	}

	root := paths.NewDataRoot(t.TempDir())
	s := store.NewFSStore(root)
	id := identity.NewWriter(s, root.Platform("gws").AccountFromSlug("test").Identity())
	account := root.Platform("gws").AccountFromSlug("test")

	// --- Create test events ---
	oneoffID := createEvent(t, `{
		"summary": "pigeon-test-oneoff",
		"start": {"dateTime": "`+futureTime(1, 10)+`"},
		"end":   {"dateTime": "`+futureTime(1, 11)+`"}
	}`)
	t.Cleanup(func() { deleteEvent(t, oneoffID) })

	recurID := createEvent(t, `{
		"summary": "pigeon-test-recurring",
		"start": {"dateTime": "`+futureTime(1, 14)+`", "timeZone": "UTC"},
		"end":   {"dateTime": "`+futureTime(1, 15)+`", "timeZone": "UTC"},
		"recurrence": ["RRULE:FREQ=DAILY;COUNT=5"]
	}`)
	t.Cleanup(func() { deleteEvent(t, recurID) })

	// Wait for API propagation.
	t.Log("waiting 3s for API propagation")
	time.Sleep(3 * time.Second)

	// --- Phase 1: Seed ---
	t.Log("=== Phase 1: Seed ===")
	cursors, err := s.LoadGWSCursors(account)
	if err != nil {
		t.Fatalf("load cursors: %v", err)
	}

	if _, err := poller.PollCalendar(s, account, cursors, id); err != nil {
		t.Fatalf("seed poll: %v", err)
	}
	if err := s.SaveGWSCursors(account, cursors); err != nil {
		t.Fatalf("save cursors: %v", err)
	}

	// Verify cursor state.
	cur := cursors.Calendar["primary"]
	if cur == nil {
		t.Fatal("no calendar cursor after seed")
	}
	if cur.SyncToken == "" {
		t.Fatal("no sync token after seed")
	}
	if cur.ExpandedUntil == "" {
		t.Fatal("no expanded_until after seed")
	}
	t.Logf("sync_token: %.30s...", cur.SyncToken)
	t.Logf("expanded_until: %s", cur.ExpandedUntil)
	t.Logf("recurring_events: %d", len(cur.RecurringEvents))

	// Verify the recurring event ID is tracked.
	if !slices.Contains(cur.RecurringEvents, recurID) {
		t.Errorf("recurring_events does not contain %s", recurID)
	}

	// Verify events landed on disk.
	calDir := account.Calendar("primary").Path()
	allEvents := readAllEvents(t, s, calDir)

	if !hasEventWithSummary(allEvents, "pigeon-test-oneoff") {
		t.Error("one-off event not found on disk")
	}

	// Recurring event should have expanded instances (DAILY;COUNT=5).
	recurInstances := eventsWithPrefix(allEvents, recurID+"_")
	t.Logf("recurring instances on disk: %d", len(recurInstances))
	if len(recurInstances) < 3 {
		t.Errorf("expected at least 3 recurring instances, got %d", len(recurInstances))
	}

	// --- Phase 2: Modify one instance, incremental sync ---
	t.Log("=== Phase 2: Modify instance, incremental sync ===")
	instanceID := recurID + "_" + futureTimeCompact(2, 14)
	patchInstance(t, instanceID, `{"summary": "pigeon-test-modified"}`)

	time.Sleep(3 * time.Second)

	if _, err := poller.PollCalendar(s, account, cursors, id); err != nil {
		t.Fatalf("incremental poll: %v", err)
	}
	if err := s.SaveGWSCursors(account, cursors); err != nil {
		t.Fatalf("save cursors: %v", err)
	}

	// Verify the modified instance is on disk.
	allEvents = readAllEvents(t, s, calDir)
	if !hasEventWithSummary(allEvents, "pigeon-test-modified") {
		t.Error("modified instance not found on disk after incremental sync")
	}

	// --- Phase 3: Second incremental poll (should be quiet) ---
	t.Log("=== Phase 3: Quiet poll ===")
	if _, err := poller.PollCalendar(s, account, cursors, id); err != nil {
		t.Errorf("quiet poll: %v", err)
	}

	t.Log("=== All phases passed ===")
}

// --- helpers ---

func futureTime(daysFromNow, hour int) string {
	t := time.Now().UTC().AddDate(0, 0, daysFromNow)
	return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func futureTimeCompact(daysFromNow, hour int) string {
	t := time.Now().UTC().AddDate(0, 0, daysFromNow)
	return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, time.UTC).Format("20060102T150405Z")
}

func createEvent(t *testing.T, body string) string {
	t.Helper()
	out, err := exec.Command("gws", "calendar", "events", "insert",
		"--params", `{"calendarId":"primary"}`,
		"--json", body).Output()
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	t.Logf("created event: %s", resp.ID)
	return resp.ID
}

func deleteEvent(t *testing.T, eventID string) {
	t.Helper()
	exec.Command("gws", "calendar", "events", "delete",
		"--params", `{"calendarId":"primary","eventId":"`+eventID+`"}`).Run()
	t.Logf("deleted event: %s", eventID)
}

func patchInstance(t *testing.T, instanceID, body string) {
	t.Helper()
	out, err := exec.Command("gws", "calendar", "events", "patch",
		"--params", `{"calendarId":"primary","eventId":"`+instanceID+`"}`,
		"--json", body).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("patch instance %s: %v\nstderr: %s", instanceID, err, exitErr.Stderr)
		}
		t.Fatalf("patch instance %s: %v", instanceID, err)
	}
	var resp struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("parse patch response: %v", err)
	}
	t.Logf("patched instance %s → %q", instanceID, resp.Summary)
}

func readAllEvents(t *testing.T, s *store.FSStore, calDir string) []*modelv1.CalendarEvent {
	t.Helper()
	var events []*modelv1.CalendarEvent
	filepath.Walk(calDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		lines, readErr := s.ReadLines(paths.DateFile(path))
		if readErr != nil {
			t.Logf("warning: read %s: %v", path, readErr)
		}
		for _, line := range lines {
			if line.Event != nil {
				events = append(events, line.Event)
			}
		}
		return nil
	})
	return events
}

func hasEventWithSummary(events []*modelv1.CalendarEvent, summary string) bool {
	for _, e := range events {
		if e.Runtime.Summary == summary {
			return true
		}
	}
	return false
}

func eventsWithPrefix(events []*modelv1.CalendarEvent, prefix string) []*modelv1.CalendarEvent {
	var matched []*modelv1.CalendarEvent
	for _, e := range events {
		if strings.HasPrefix(e.Runtime.Id, prefix) {
			matched = append(matched, e)
		}
	}
	return matched
}
