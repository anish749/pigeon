package model

import (
	"log/slog"

	calendar "google.golang.org/api/calendar/v3"
)

// EventDateForStorage returns the YYYY-MM-DD date for filing a calendar event
// into a per-day log file. Priority: Start > OriginalStartTime > Updated.
func EventDateForStorage(ev *calendar.Event) string {
	if ev.Start != nil {
		if d := dateFromRFC3339(ev.Start.DateTime); d != "" {
			return d
		}
		if ev.Start.Date != "" {
			return ev.Start.Date
		}
	}
	// Cancelled recurring instances carry the original start instead of start/end.
	if ev.OriginalStartTime != nil {
		if d := dateFromRFC3339(ev.OriginalStartTime.DateTime); d != "" {
			return d
		}
		if ev.OriginalStartTime.Date != "" {
			return ev.OriginalStartTime.Date
		}
	}
	if d := dateFromRFC3339(ev.Updated); d != "" {
		slog.Warn("calendar event has no start time, falling back to updated",
			"event_id", ev.Id, "status", ev.Status)
		return d
	}
	slog.Warn("calendar event has no parseable date, filing under unknown",
		"event_id", ev.Id, "status", ev.Status)
	return "unknown"
}

// dateFromRFC3339 extracts YYYY-MM-DD from an RFC 3339 datetime string.
// Returns "" if s is empty or doesn't contain a 'T' at position 10.
func dateFromRFC3339(s string) string {
	if len(s) >= 11 && s[10] == 'T' {
		return s[:10]
	}
	return ""
}
