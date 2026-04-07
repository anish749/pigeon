package poller

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/gws/calendar"
	"github.com/anish749/pigeon/internal/gws/gwsstore"
	"github.com/anish749/pigeon/internal/gws/model"
)

// PollCalendar polls for calendar changes and stores events as JSONL.
func PollCalendar(accountDir string, cursors *gwsstore.Cursors) error {
	const calID = "primary"

	if cursors.Calendar == nil {
		cursors.Calendar = make(gwsstore.CalendarCursors)
	}

	syncToken := cursors.Calendar[calID]

	// Seed the sync token if we don't have one yet.
	if syncToken == "" {
		slog.Info("seeding calendar sync token", "calendar", calID)
		token, err := calendar.SeedSyncToken(calID)
		if err != nil {
			return fmt.Errorf("seed calendar %s: %w", calID, err)
		}
		cursors.Calendar[calID] = token
		slog.Info("seeded calendar sync token", "calendar", calID)
		return nil
	}

	events, newToken, err := calendar.ListEvents(calID, syncToken)
	if err != nil {
		return fmt.Errorf("poll calendar %s: %w", calID, err)
	}

	var errs []error
	for _, ev := range events {
		datePath := eventDateFile(accountDir, calID, ev)
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

// eventDateFile returns the JSONL file path for an event based on its start date.
// Path: {accountDir}/gcalendar/{calID}/{YYYY-MM-DD}.jsonl
func eventDateFile(accountDir, calID string, ev model.EventLine) string {
	date := eventDate(ev)
	return filepath.Join(accountDir, "gcalendar", calID, date+".jsonl")
}

// eventDate extracts the date string (YYYY-MM-DD) from an event.
// Timed events: parse date from Start (RFC 3339 datetime).
// All-day events: use StartDate directly.
// Cancelled events with no start info: parse date from Updated.
func eventDate(ev model.EventLine) string {
	if ev.Start != "" {
		if d := parseDateFromDateTime(ev.Start); d != "" {
			return d
		}
	}
	if ev.StartDate != "" {
		return ev.StartDate
	}
	// Cancelled events may lack start info; fall back to Updated timestamp.
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
