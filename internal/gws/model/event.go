package model

import "log/slog"

// EventLine represents a Google Calendar event in JSONL format.
type EventLine struct {
	Type        string   `json:"type"`                  // always "event"
	ID          string   `json:"id"`                    // Calendar event ID
	Ts          string   `json:"ts"`                    // created time (RFC 3339)
	Updated     string   `json:"updated"`               // last modified (RFC 3339)
	Status      string   `json:"status"`                // confirmed, tentative, cancelled
	Summary     string   `json:"summary"`               // event title
	Description string   `json:"description,omitempty"` // event notes
	Start       string   `json:"start,omitempty"`       // RFC 3339 datetime (timed)
	End         string   `json:"end,omitempty"`         // RFC 3339 datetime (timed)
	StartDate   string   `json:"startDate,omitempty"`   // YYYY-MM-DD (all-day)
	EndDate     string   `json:"endDate,omitempty"`     // YYYY-MM-DD (all-day)
	Location    string   `json:"location,omitempty"`    // event location
	Organizer   string   `json:"organizer,omitempty"`   // organizer email
	Attendees   []string `json:"attendees,omitempty"`   // attendee emails
	MeetLink          string   `json:"meetLink,omitempty"`          // Google Meet link
	EventType         string   `json:"eventType"`                   // default, focusTime, etc.
	Recurring         bool     `json:"recurring,omitempty"`         // recurring event instance
	OriginalStartTime string   `json:"originalStartTime,omitempty"` // original start for cancelled recurring instances
}

// DateForStorage returns the YYYY-MM-DD date for filing this event into a
// per-day log file. Priority: Start > StartDate > OriginalStartTime > Updated.
func (e EventLine) DateForStorage() string {
	if d := dateFromRFC3339(e.Start); d != "" {
		return d
	}
	if e.StartDate != "" {
		return e.StartDate
	}
	// Cancelled recurring instances carry the original start instead of start/end.
	if d := dateFromRFC3339(e.OriginalStartTime); d != "" {
		return d
	}
	if e.OriginalStartTime != "" && len(e.OriginalStartTime) >= 10 {
		return e.OriginalStartTime[:10]
	}
	if d := dateFromRFC3339(e.Updated); d != "" {
		return d
	}
	slog.Warn("calendar event has no parseable date, filing under unknown",
		"event_id", e.ID, "status", e.Status)
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
