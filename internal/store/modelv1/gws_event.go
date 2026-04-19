package modelv1

import (
	"log/slog"
	"time"

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

// DatesForStorage returns the YYYY-MM-DD dates for filing a calendar event
// into per-day log files. Multi-day events return one date per day spanned.
// For all-day events the end date is exclusive (per Google Calendar API).
// Falls back to DateForStorage if the range cannot be determined.
func (e *CalendarEvent) DatesForStorage() []string {
	if e.Runtime.Start == nil {
		return []string{e.DateForStorage()}
	}

	// All-day events: Start.Date through End.Date (end exclusive).
	if e.Runtime.Start.Date != "" {
		endDate := ""
		if e.Runtime.End != nil {
			endDate = e.Runtime.End.Date
		}
		if endDate == "" {
			return []string{e.Runtime.Start.Date}
		}
		return dateRange(e.Runtime.Start.Date, endDate)
	}

	// Timed events: derive date range from Start.DateTime through End.DateTime.
	startDate := dateFromRFC3339(e.Runtime.Start.DateTime)
	if startDate == "" {
		return []string{e.DateForStorage()}
	}
	endDate := ""
	if e.Runtime.End != nil {
		endDate = dateFromRFC3339(e.Runtime.End.DateTime)
	}
	if endDate == "" || endDate == startDate {
		return []string{startDate}
	}
	// For timed events the end is inclusive of that moment, so include the end date.
	return dateRangeInclusive(startDate, endDate)
}

// dateRange returns YYYY-MM-DD strings from start (inclusive) to end (exclusive).
// Returns a single-element slice with start if parsing fails.
func dateRange(start, end string) []string {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return []string{start}
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return []string{start}
	}
	var dates []string
	for d := s; d.Before(e); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	if len(dates) == 0 {
		return []string{start}
	}
	return dates
}

// dateRangeInclusive returns YYYY-MM-DD strings from start to end, both inclusive.
// Returns a single-element slice with start if parsing fails.
func dateRangeInclusive(start, end string) []string {
	s, err := time.Parse("2006-01-02", start)
	if err != nil {
		return []string{start}
	}
	e, err := time.Parse("2006-01-02", end)
	if err != nil {
		return []string{start}
	}
	var dates []string
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	if len(dates) == 0 {
		return []string{start}
	}
	return dates
}

// dateFromRFC3339 extracts YYYY-MM-DD from an RFC 3339 datetime string.
// Returns "" if s is empty or doesn't contain a 'T' at position 10.
func dateFromRFC3339(s string) string {
	if len(s) >= 11 && s[10] == 'T' {
		return s[:10]
	}
	return ""
}
