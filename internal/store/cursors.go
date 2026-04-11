package store

// GWSCursors holds polling cursors for Google Workspace services. The daemon
// loads and saves them via FSStore.LoadGWSCursors / SaveGWSCursors so that
// the poller can resume where it left off across restarts.
type GWSCursors struct {
	Gmail    GmailCursors       `yaml:"gmail,omitempty"`
	Drive    GWSDriveCursors    `yaml:"drive,omitempty"`
	Calendar GWSCalendarCursors `yaml:"calendar,omitempty"`
}

// GmailCursors holds the Gmail history cursor.
type GmailCursors struct {
	HistoryID string `yaml:"history_id,omitempty"`
}

// GWSDriveCursors holds the Drive changes cursor.
type GWSDriveCursors struct {
	PageToken string `yaml:"page_token,omitempty"`
}

// GWSCalendarCursor holds the sync state for a single calendar.
type GWSCalendarCursor struct {
	SyncToken       string   `yaml:"sync_token,omitempty"`
	ExpandedUntil   string   `yaml:"expanded_until,omitempty"`
	RecurringEvents []string `yaml:"recurring_events,omitempty"`
}

// GWSCalendarCursors maps calendar ID to its cursor.
type GWSCalendarCursors map[string]*GWSCalendarCursor

// LinearCursors holds polling cursors for a Linear workspace. The daemon
// loads and saves them via FSStore.LoadLinearCursors / SaveLinearCursors so
// that the poller can resume where it left off across restarts.
type LinearCursors struct {
	Issues LinearIssueCursor `yaml:"issues,omitempty"`
}

// LinearIssueCursor holds the incremental sync cursor for Linear issues.
type LinearIssueCursor struct {
	UpdatedAfter string `yaml:"updated_after,omitempty"`
}
