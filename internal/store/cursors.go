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

// JiraCursors holds polling cursors for a single Jira project. The daemon
// loads and saves them via FSStore.LoadJiraCursors / SaveJiraCursors so that
// the poller can resume where it left off across restarts. Each project on
// each site has its own cursor file, since the JQL incremental query
// (`updated > "<cursor>"`) is project-scoped.
type JiraCursors struct {
	Issues JiraIssueCursor `yaml:"issues,omitempty"`
}

// JiraIssueCursor holds the incremental sync cursor for Jira issues.
// UpdatedAfter is stored as RFC 3339 (the same form Jira returns in
// fields.updated). The poller reformats it to "yyyy-MM-dd HH:mm" before
// interpolating into JQL — JQL silently returns zero matches for any other
// date format, so this conversion is mandatory.
type JiraIssueCursor struct {
	UpdatedAfter string `yaml:"updated_after,omitempty"`
}

// SlackCursors maps channel ID to the last synced Slack message timestamp.
// The daemon loads and saves them via FSStore.LoadSlackCursors / SaveSlackCursors
// so that sync and the real-time listener can resume where they left off.
type SlackCursors map[string]string
