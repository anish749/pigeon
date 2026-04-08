package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/gws"
	"github.com/anish749/pigeon/internal/gws/calendar"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// PollCalendar polls for calendar changes and stores events as JSONL.
func PollCalendar(account paths.AccountDir, cursors *gwsstore.Cursors) error {
	const calID = "primary"

	if cursors.Calendar == nil {
		cursors.Calendar = make(gwsstore.CalendarCursors)
	}

	syncToken := cursors.Calendar[calID]

	// Seed the sync token if we don't have one yet.
	// Seeding fetches events in a ±90-day window as a backfill.
	if syncToken == "" {
		slog.Info("seeding calendar sync token", "calendar", calID)
		events, token, err := calendar.SeedSyncToken(calID)
		if err != nil {
			return fmt.Errorf("seed calendar %s: %w", calID, err)
		}

		var errs []error
		for _, ev := range events {
			datePath := account.Calendar(calID).DateFile(eventDate(ev))
			line := model.Line{Type: "event", Event: &ev}
			if err := gwsstore.AppendLine(datePath, line); err != nil {
				errs = append(errs, fmt.Errorf("append event %s: %w", ev.ID, err))
			}
		}

		cursors.Calendar[calID] = token
		slog.Info("seeded calendar with backfill", "calendar", calID, "events", len(events))
		return errors.Join(errs...)
	}

	events, newToken, err := calendar.ListEvents(calID, syncToken)
	if err != nil {
		if gws.IsCursorExpired(err) {
			slog.Warn("calendar sync token expired, will re-seed", "calendar", calID)
			cursors.Calendar[calID] = ""
			return nil
		}
		return fmt.Errorf("poll calendar %s: %w", calID, err)
	}

	var errs []error
	for _, ev := range events {
		datePath := account.Calendar(calID).DateFile(eventDate(ev))
		line := model.Line{Type: "event", Event: &ev}
		if err := gwsstore.AppendLine(datePath, line); err != nil {
			errs = append(errs, fmt.Errorf("append event %s: %w", ev.ID, err))
		}
	}

	if len(events) > 0 {
		slog.Info("polled calendar", "calendar", calID, "events", len(events))
	}

	cursors.Calendar[calID] = newToken
	return errors.Join(errs...)
}

// eventDate extracts the date string (YYYY-MM-DD) from an event.
// Priority: Start > StartDate > OriginalStartTime > Updated > "unknown".
func eventDate(ev model.EventLine) string {
	if ev.Start != "" {
		if d := parseDateFromDateTime(ev.Start); d != "" {
			return d
		}
	}
	if ev.StartDate != "" {
		return ev.StartDate
	}
	// Cancelled recurring instances carry the original start instead of start/end.
	if ev.OriginalStartTime != "" {
		if d := parseDateFromDateTime(ev.OriginalStartTime); d != "" {
			return d
		}
		// OriginalStartTime may be a bare date (YYYY-MM-DD) for all-day events.
		if len(ev.OriginalStartTime) == 10 {
			return ev.OriginalStartTime
		}
	}
	if ev.Updated != "" {
		if d := parseDateFromDateTime(ev.Updated); d != "" {
			return d
		}
	}
	slog.Warn("calendar event has no parseable date, filing under unknown",
		"event_id", ev.ID, "status", ev.Status)
	return "unknown"
}

// parseDateFromDateTime extracts YYYY-MM-DD from an RFC 3339 datetime string.
func parseDateFromDateTime(dt string) string {
	// Try parsing as RFC 3339.
	t, err := time.Parse(time.RFC3339, dt)
	if err != nil {
		// Fall back: take everything before the first 'T'.
		if i := strings.IndexByte(dt, 'T'); i == 10 {
			return dt[:10]
		}
		return ""
	}
	return t.Format("2006-01-02")
}
