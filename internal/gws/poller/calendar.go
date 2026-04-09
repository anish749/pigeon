package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/calendar"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// PollCalendar runs the calendar sync cycle: seed, incremental sync, and
// window expansion for recurring events.
func PollCalendar(account paths.AccountDir, cursors *gwsstore.Cursors) error {
	const calID = "primary"

	if cursors.Calendar == nil {
		cursors.Calendar = make(gwsstore.CalendarCursors)
	}

	cur := cursors.Calendar[calID]

	// Phase 1: Seed — no cursor exists yet.
	if cur == nil || cur.SyncToken == "" {
		return seedCalendar(account, cursors, calID)
	}

	// Phase 2: Incremental sync.
	if err := syncCalendar(account, cur, calID); err != nil {
		if gws.IsCursorExpired(err) {
			slog.Warn("calendar sync token expired, will re-seed", "calendar", calID)
			cursors.Calendar[calID] = nil
			return nil
		}
		return err
	}

	// Phase 3: Window expansion — extend recurring event instances if
	// expanded_until is within ExpansionThresholdDays of now.
	return maybeExpandWindow(account, cur, calID)
}

// seedCalendar performs the initial calendar sync: fetches all events from
// BackfillDays ago onward, expands recurring events within ±BackfillDays,
// and writes everything to disk.
func seedCalendar(account paths.AccountDir, cursors *gwsstore.Cursors, calID string) error {
	slog.Info("seeding calendar", "calendar", calID)

	result, err := calendar.SeedSyncToken(calID)
	if err != nil {
		return fmt.Errorf("seed calendar %s: %w", calID, err)
	}

	// Write one-off events and exception instances to disk.
	errs := writeEvents(account, calID, result.Events)

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
		errs = append(errs, writeEvents(account, calID, instances)...)
	}

	cursors.Calendar[calID] = &gwsstore.CalendarCursor{
		SyncToken:       result.SyncToken,
		ExpandedUntil:   timeMax,
		RecurringEvents: result.RecurringIDs,
	}
	slog.Info("seeded calendar with backfill",
		"calendar", calID,
		"events", len(result.Events),
		"recurring", len(result.RecurringIDs))

	return errors.Join(errs...)
}

// syncCalendar performs an incremental sync: fetches changes since the last
// sync token, writes events to disk, and re-expands any changed recurring parents.
func syncCalendar(account paths.AccountDir, cur *gwsstore.CalendarCursor, calID string) error {
	result, err := calendar.ListEvents(calID, cur.SyncToken)
	if err != nil {
		return fmt.Errorf("poll calendar %s: %w", calID, err)
	}

	// Write one-off events and changed instances to disk.
	errs := writeEvents(account, calID, result.Events)

	// Re-expand any recurring parents that changed.
	now := time.Now().UTC()
	timeMin := now.AddDate(0, 0, -gws.BackfillDays).Format(time.RFC3339)
	for _, recurID := range result.RecurringIDs {
		instances, err := calendar.ListInstances(calID, recurID, timeMin, cur.ExpandedUntil)
		if err != nil {
			errs = append(errs, fmt.Errorf("expand %s: %w", recurID, err))
			continue
		}
		errs = append(errs, writeEvents(account, calID, instances)...)
	}

	// Track newly discovered recurring events and remove deleted ones.
	cur.RecurringEvents = mergeRecurringIDs(cur.RecurringEvents, result.RecurringIDs)
	cur.RecurringEvents = removeRecurringIDs(cur.RecurringEvents, result.CancelledRecurringIDs)
	cur.SyncToken = result.SyncToken

	if len(result.Events) > 0 || len(result.RecurringIDs) > 0 {
		slog.Info("polled calendar",
			"calendar", calID,
			"events", len(result.Events),
			"recurring_changed", len(result.RecurringIDs))
	}

	return errors.Join(errs...)
}

// maybeExpandWindow checks if the expansion window needs extending and, if so,
// fetches new instances for all known recurring events.
func maybeExpandWindow(account paths.AccountDir, cur *gwsstore.CalendarCursor, calID string) error {
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
		errs = append(errs, writeEvents(account, calID, instances)...)
	}

	cur.ExpandedUntil = newTimeMax
	return errors.Join(errs...)
}

// writeEvents appends events to their date-partitioned JSONL files.
func writeEvents(account paths.AccountDir, calID string, events []model.EventLine) []error {
	var errs []error
	for _, ev := range events {
		datePath := account.Calendar(calID).DateFile(ev.DateForStorage())
		line := model.Line{Type: "event", Event: &ev}
		if err := gwsstore.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append event %s: %w", ev.ID, err))
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

