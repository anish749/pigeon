package model

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
