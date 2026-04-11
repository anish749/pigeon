package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/calendar"
	"github.com/anish749/pigeon/internal/identity"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// PollCalendar runs the calendar sync cycle: seed, incremental sync, and
// window expansion for recurring events. Returns the number of changes
// observed (events + recurring events changed) plus any error.
func PollCalendar(s *store.FSStore, account paths.AccountDir, cursors *store.GWSCursors, id *identity.Service) (int, error) {
	const calID = "primary"

	if cursors.Calendar == nil {
		cursors.Calendar = make(store.GWSCalendarCursors)
	}

	cur := cursors.Calendar[calID]

	// Phase 1: Seed — no cursor exists yet.
	if cur == nil || cur.SyncToken == "" {
		return seedCalendar(s, account, cursors, calID, id)
	}

	// Phase 2: Incremental sync.
	changes, err := syncCalendar(s, account, cur, calID, id)
	if err != nil {
		if gws.IsCursorExpired(err) {
			slog.Warn("calendar sync token expired, will re-seed", "calendar", calID)
			cursors.Calendar[calID] = nil
			return 0, nil
		}
		return changes, err
	}

	// Phase 3: Window expansion — extend recurring event instances if
	// expanded_until is within ExpansionThresholdDays of now. Window
	// expansion writes events to disk but is not an observed "change"
	// from Google's perspective, so we don't add it to the changes count.
	if err := maybeExpandWindow(s, account, cur, calID); err != nil {
		return changes, err
	}
	return changes, nil
}

// seedCalendar performs the initial calendar sync: fetches all events from
// BackfillDays ago onward, expands recurring events within ±BackfillDays,
// and writes everything to disk. Returns the number of seeded events
// (one-off + instances) plus any error.
func seedCalendar(s *store.FSStore, account paths.AccountDir, cursors *store.GWSCursors, calID string, id *identity.Service) (int, error) {
	slog.Info("seeding calendar", "calendar", calID)

	result, err := calendar.SeedSyncToken(calID)
	if err != nil {
		return 0, fmt.Errorf("seed calendar %s: %w", calID, err)
	}

	// Write one-off events and exception instances to disk.
	errs := writeEvents(s, account, calID, result.Events, id)

	// Expand recurring events within the backfill window.
	now := time.Now().UTC()
	timeMin := now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339)
	timeMax := now.AddDate(0, 0, gws.BackfillDays).Format(time.RFC3339)

	for _, recurID := range result.RecurringIDs {
		instances, err := calendar.ListInstances(calID, recurID, timeMin, timeMax)
		if err != nil {
			errs = append(errs, fmt.Errorf("expand %s: %w", recurID, err))
			continue
		}
		errs = append(errs, writeEvents(s, account, calID, instances, id)...)
	}

	cursors.Calendar[calID] = &store.GWSCalendarCursor{
		SyncToken:       result.SyncToken,
		ExpandedUntil:   timeMax,
		RecurringEvents: result.RecurringIDs,
	}
	slog.Info("seeded calendar with backfill",
		"calendar", calID,
		"events", len(result.Events),
		"recurring", len(result.RecurringIDs))

	return len(result.Events) + len(result.RecurringIDs), errors.Join(errs...)
}

// syncCalendar performs an incremental sync: fetches changes since the last
// sync token, writes events to disk, and re-expands any changed recurring parents.
// Returns the number of changed events (one-off + recurring parents) plus any error.
func syncCalendar(s *store.FSStore, account paths.AccountDir, cur *store.GWSCalendarCursor, calID string, id *identity.Service) (int, error) {
	result, err := calendar.ListEvents(calID, cur.SyncToken)
	if err != nil {
		return 0, fmt.Errorf("poll calendar %s: %w", calID, err)
	}

	// Write one-off events and changed instances to disk.
	errs := writeEvents(s, account, calID, result.Events, id)

	// Re-expand any recurring parents that changed.
	now := time.Now().UTC()
	timeMin := now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339)
	for _, recurID := range result.RecurringIDs {
		instances, err := calendar.ListInstances(calID, recurID, timeMin, cur.ExpandedUntil)
		if err != nil {
			errs = append(errs, fmt.Errorf("expand %s: %w", recurID, err))
			continue
		}
		errs = append(errs, writeEvents(s, account, calID, instances, id)...)
	}

	// Track newly discovered recurring events and remove deleted ones.
	cur.RecurringEvents = mergeRecurringIDs(cur.RecurringEvents, result.RecurringIDs)
	cur.RecurringEvents = removeRecurringIDs(cur.RecurringEvents, result.CancelledRecurringIDs)
	cur.SyncToken = result.SyncToken

	changes := len(result.Events) + len(result.RecurringIDs)
	if changes > 0 {
		slog.Info("polled calendar",
			"calendar", calID,
			"events", len(result.Events),
			"recurring_changed", len(result.RecurringIDs))
	}

	return changes, errors.Join(errs...)
}

// maybeExpandWindow checks if the expansion window needs extending and, if so,
// fetches new instances for all known recurring events.
func maybeExpandWindow(s *store.FSStore, account paths.AccountDir, cur *store.GWSCalendarCursor, calID string) error {
	if cur.ExpandedUntil == "" || len(cur.RecurringEvents) == 0 {
		return nil
	}

	expandedUntil, err := time.Parse(time.RFC3339, cur.ExpandedUntil)
	if err != nil {
		return fmt.Errorf("parse expanded_until: %w", err)
	}

	threshold := time.Now().UTC().AddDate(0, 0, gws.ExpansionThresholdDays)
	if expandedUntil.After(threshold) {
		return nil // window is still far enough ahead
	}

	newTimeMax := time.Now().UTC().AddDate(0, 0, gws.BackfillDays).Format(time.RFC3339)
	slog.Info("expanding calendar window",
		"calendar", calID,
		"from", cur.ExpandedUntil,
		"to", newTimeMax,
		"recurring_events", len(cur.RecurringEvents))

	var errs []error
	for _, recurID := range cur.RecurringEvents {
		instances, err := calendar.ListInstances(calID, recurID, cur.ExpandedUntil, newTimeMax)
		if err != nil {
			errs = append(errs, fmt.Errorf("expand window %s: %w", recurID, err))
			continue
		}
		errs = append(errs, writeEvents(s, account, calID, instances, nil)...)
	}

	cur.ExpandedUntil = newTimeMax
	return errors.Join(errs...)
}

// writeEvents appends events to their date-partitioned JSONL files and
// pushes identity signals from event attendees.
func writeEvents(s *store.FSStore, account paths.AccountDir, calID string, events []*modelv1.CalendarEvent, id *identity.Service) []error {
	var errs []error
	var signals []identity.Signal
	for _, ev := range events {
		datePath := account.Calendar(calID).DateFile(ev.DateForStorage())
		line := modelv1.Line{Type: modelv1.LineEvent, Event: ev}
		if err := s.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append event %s: %w", ev.Runtime.Id, err))
		}

		// Collect attendee identity signals.
		if id != nil {
			for _, att := range ev.Runtime.Attendees {
				if att.Email != "" {
					signals = append(signals, identity.Signal{
						Email: att.Email,
						Name:  att.DisplayName,
					})
				}
			}
		}
	}

	if id != nil && len(signals) > 0 {
		if err := id.ObserveBatch(signals); err != nil {
			slog.Warn("identity: calendar signal batch failed", "error", err)
		}
	}
	return errs
}

// mergeRecurringIDs adds new IDs to the existing list, deduplicating.
func mergeRecurringIDs(existing, additions []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, id := range existing {
		seen[id] = true
	}
	merged := existing
	for _, id := range additions {
		if !seen[id] {
			merged = append(merged, id)
			seen[id] = true
		}
	}
	return merged
}

// removeRecurringIDs removes cancelled IDs from the tracked list.
func removeRecurringIDs(existing, removals []string) []string {
	if len(removals) == 0 {
		return existing
	}
	remove := make(map[string]bool, len(removals))
	for _, id := range removals {
		remove[id] = true
	}
	var kept []string
	for _, id := range existing {
		if !remove[id] {
			kept = append(kept, id)
		}
	}
	return kept
}
