package paths

// LogFile is a sealed interface for typed file paths that hold JSONL log data.
// The unexported method restricts implementations to this package.
type LogFile interface {
	Path() string
	logFile()
}

// ContentFile is a sealed interface for typed file paths that hold
// document content (markdown, CSV). Separate from DataFile because
// these are written atomically rather than appended to.
type ContentFile interface {
	Path() string
	contentFile()
}

// Compile-time interface guards.
var (
	_ LogFile = MessagingDateFile("")
	_ LogFile = EmailDateFile("")
	_ LogFile = CalendarDateFile("")
	_ LogFile = ThreadFile("")
	_ LogFile = CommentsFile("")
	_ ContentFile = TabFile("")
	_ ContentFile = SheetFile("")
	_ ContentFile = FormulaFile("")
)

// MessagingDateFile is a path to a daily JSONL file in a messaging
// conversation (slack, whatsapp): <root>/<plat>/<acct>/<conv>/YYYY-MM-DD.jsonl.
// Lines carry a top-level "ts" field; conversations group by parent dir.
type MessagingDateFile string

// Path returns the file path as a string.
func (d MessagingDateFile) Path() string { return string(d) }
func (MessagingDateFile) logFile()       {}

// EmailDateFile is a path to a daily JSONL file under a Gmail account:
// <root>/gws/<acct>/gmail/YYYY-MM-DD.jsonl.
// Lines carry a top-level "ts" field; the account is the conversation unit.
type EmailDateFile string

// Path returns the file path as a string.
func (d EmailDateFile) Path() string { return string(d) }
func (EmailDateFile) logFile()       {}

// CalendarDateFile is a path to a daily JSONL file under a single calendar:
// <root>/gws/<acct>/gcalendar/<calID>/YYYY-MM-DD.jsonl.
// Lines carry "updated" and "created" fields; the calendar id is the
// conversation unit.
type CalendarDateFile string

// Path returns the file path as a string.
func (d CalendarDateFile) Path() string { return string(d) }
func (CalendarDateFile) logFile()       {}

// ThreadFile is a path to a thread's JSONL file.
type ThreadFile string

// Path returns the file path as a string.
func (t ThreadFile) Path() string { return string(t) }
func (ThreadFile) logFile()      {}

// CommentsFile is a path to a Drive file's comments JSONL.
type CommentsFile string

// Path returns the file path as a string.
func (c CommentsFile) Path() string { return string(c) }
func (CommentsFile) logFile()      {}

// ConvMetaFile is a path to a conversation's .meta.json sidecar (messaging data).
// Drive file metadata uses DriveMetaFile (see paths/gws.go) which carries the
// modification date in the filename and needs separate dir/name handling.
type ConvMetaFile string

// Path returns the file path as a string.
func (m ConvMetaFile) Path() string { return string(m) }

// TabFile is a path to a document tab's markdown content.
type TabFile string

// Path returns the file path as a string.
func (t TabFile) Path() string { return string(t) }
func (TabFile) contentFile()   {}

// SheetFile is a path to a sheet's CSV export.
type SheetFile string

// Path returns the file path as a string.
func (s SheetFile) Path() string { return string(s) }
func (SheetFile) contentFile()   {}

// FormulaFile is a path to a sheet's formulas CSV export.
type FormulaFile string

// Path returns the file path as a string.
func (f FormulaFile) Path() string { return string(f) }
func (FormulaFile) contentFile()   {}

// AttachmentFile is a path to an inline image or attachment.
type AttachmentFile string

// Path returns the file path as a string.
func (a AttachmentFile) Path() string { return string(a) }

// MaintenanceFile is a path to an account's .maintenance.json state sidecar.
type MaintenanceFile string

// Path returns the file path as a string.
func (m MaintenanceFile) Path() string { return string(m) }

// SyncCursorsFile is a path to an account's .sync-cursors.yaml file.
type SyncCursorsFile string

// Path returns the file path as a string.
func (s SyncCursorsFile) Path() string { return string(s) }

// PollMetricsFile is a path to an account's .poll-metrics.jsonl operational
// log, appended one record per poll per service.
type PollMetricsFile string

// Path returns the file path as a string.
func (p PollMetricsFile) Path() string { return string(p) }

// PendingDeletesFile is a path to a Gmail account's .pending-email-deletes
// queue file, written one email ID per line by the poller and drained during
// maintenance.
type PendingDeletesFile string

// Path returns the file path as a string.
func (p PendingDeletesFile) Path() string { return string(p) }

// WorkstreamStoreDir is a path to a workspace's persistent workstream store
// directory: <base>/.workspaces/<name>/workstream/.
type WorkstreamStoreDir string

// Path returns the directory path as a string.
func (w WorkstreamStoreDir) Path() string { return string(w) }
