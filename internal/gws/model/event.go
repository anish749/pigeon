package model

import (
	"log/slog"

	calendar "google.golang.org/api/calendar/v3"
)

type CalendarEvent struct {
	Parsed calendar.Event         // This is used in the write layer for extracting info from the types.
	Raw    map[string]any // This is used for serialization. We know that calendar events are JSON structs, not arrays / strings
}

// EventDateForStorage returns the YYYY-MM-DD date for filing a calendar event
// into a per-day log file. Priority: Start > OriginalStartTime > Updated.
func (e *CalendarEvent) EventDateForStorage() string {
	if e.Parsed.Start != nil {
		if d := dateFromRFC3339(e.Parsed.Start.DateTime); d != "" {
			return d
		}
		if e.Parsed.Start.Date != "" {
			return e.Parsed.Start.Date
		}
	}
	// Cancelled recurring instances carry the original start instead of start/end.
	if e.Parsed.OriginalStartTime != nil {
		if d := dateFromRFC3339(e.Parsed.OriginalStartTime.DateTime); d != "" {
			return d
		}
		if e.Parsed.OriginalStartTime.Date != "" {
			return e.Parsed.OriginalStartTime.Date
		}
	}
	if d := dateFromRFC3339(e.Parsed.Updated); d != "" {
		slog.Warn("calendar event has no start time, falling back to updated",
			"event_id", e.Parsed.Id, "status", e.Parsed.Status)
		return d
	}
	slog.Warn("calendar event has no parseable date, filing under unknown",
		"event_id", e.Parsed.Id, "status", e.Parsed.Status)
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
