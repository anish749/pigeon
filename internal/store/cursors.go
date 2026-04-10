package store

// Cursors holds polling cursors for GWS services. The daemon loads and saves
// them via FSStore.LoadCursors / SaveCursors so that the poller can resume
// where it left off across restarts.
type Cursors struct {
	Gmail    GmailCursors    `yaml:"gmail,omitempty"`
	Drive    DriveCursors    `yaml:"drive,omitempty"`
	Calendar CalendarCursors `yaml:"calendar,omitempty"`
}

// GmailCursors holds the Gmail history cursor.
type GmailCursors struct {
	HistoryID string `yaml:"history_id,omitempty"`
}

// DriveCursors holds the Drive changes cursor.
type DriveCursors struct {
	PageToken string `yaml:"page_token,omitempty"`
}

// CalendarCursor holds the sync state for a single calendar.
type CalendarCursor struct {
	SyncToken       string   `yaml:"sync_token,omitempty"`
	ExpandedUntil   string   `yaml:"expanded_until,omitempty"`
	RecurringEvents []string `yaml:"recurring_events,omitempty"`
}

// CalendarCursors maps calendar ID to its cursor.
type CalendarCursors map[string]*CalendarCursor
