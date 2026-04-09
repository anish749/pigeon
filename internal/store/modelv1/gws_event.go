package modelv1

import (
	"log/slog"

	calendar "google.golang.org/api/calendar/v3"
)

// CalendarEvent holds two representations of a single calendar event: a
// typed view for in-process code and a raw map that is the source of truth
// for disk storage.
//
// Runtime is the typed calendar.Event used by classify, dedup, date extraction,
// and anything else that needs field access. Serialized is a JSON-shaped map
// that MarshalGWS writes verbatim to disk — it preserves every field the API
// returned, even ones the generated SDK types don't know about.
//
// Only Serialized is persisted. Mutations to Runtime are not reflected on
// disk unless they're also pushed into Serialized, so treat Runtime as a
// read-only view of the event.
type CalendarEvent struct {
	Runtime    calendar.Event
	Serialized map[string]any
}

// DateForStorage returns the YYYY-MM-DD date for filing a calendar event
// into a per-day log file. Priority: Start > OriginalStartTime > Updated.
func (e *CalendarEvent) DateForStorage() string {
	if e.Runtime.Start != nil {
		if d := dateFromRFC3339(e.Runtime.Start.DateTime); d != "" {
			return d
		}
		if e.Runtime.Start.Date != "" {
			return e.Runtime.Start.Date
		}
	}
	// Cancelled recurring instances carry the original start instead of start/end.
	if e.Runtime.OriginalStartTime != nil {
		if d := dateFromRFC3339(e.Runtime.OriginalStartTime.DateTime); d != "" {
			return d
		}
		if e.Runtime.OriginalStartTime.Date != "" {
			return e.Runtime.OriginalStartTime.Date
		}
	}
	if d := dateFromRFC3339(e.Runtime.Updated); d != "" {
		slog.Warn("calendar event has no start time, falling back to updated",
			"event_id", e.Runtime.Id, "status", e.Runtime.Status)
		return d
	}
	slog.Warn("calendar event has no parseable date, filing under unknown",
		"event_id", e.Runtime.Id, "status", e.Runtime.Status)
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
