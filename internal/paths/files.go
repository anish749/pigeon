package paths

// DataFile is the top-level sealed interface implemented by every typed file
// path under pigeon's data tree. The unexported dataFile() method restricts
// implementations to this package, so a value typed as DataFile is guaranteed
// to be one of the known shapes — every member is enumerable by grep on
// "dataFile()".
//
// LogFile and ContentFile are sub-categories that further distinguish
// append-only JSONL logs from atomically-written document content. A type can
// belong to either sub-category or to neither (sidecars, anchors, queues),
// but every typed file path is a DataFile.
type DataFile interface {
	Path() string
	dataFile()
}

// LogFile is a sealed interface for typed file paths that hold JSONL log data.
// The unexported logFile() method restricts implementations to this package.
type LogFile interface {
	DataFile
	logFile()
}

// ContentFile is a sealed interface for typed file paths that hold
// document content (markdown, CSV). Separate from LogFile because
// these are written atomically rather than appended to.
type ContentFile interface {
	DataFile
	contentFile()
}

// Compile-time interface guards. Every typed file path must implement
// DataFile; LogFile and ContentFile transitively imply DataFile, so the
// JSONL/content kinds only need their sub-category assertion. The remaining
// standalone kinds (no sub-category) are asserted directly against DataFile.
var (
	_ LogFile     = MessagingDateFile("")
	_ LogFile     = EmailDateFile("")
	_ LogFile     = CalendarDateFile("")
	_ LogFile     = ThreadFile("")
	_ LogFile     = CommentsFile("")
	_ ContentFile = TabFile("")
	_ ContentFile = SheetFile("")
	_ ContentFile = FormulaFile("")

	_ DataFile = AttachmentFile("")
	_ DataFile = ConvMetaFile("")
	_ DataFile = PeopleFile("")
	_ DataFile = MaintenanceFile("")
	_ DataFile = SyncCursorsFile("")
	_ DataFile = PollMetricsFile("")
	_ DataFile = PendingDeletesFile("")
	_ DataFile = DriveMetaFile{}
)

// MessagingDateFile is a path to a daily JSONL file in a messaging
// conversation (slack, whatsapp): <root>/<plat>/<acct>/<conv>/YYYY-MM-DD.jsonl.
// Lines carry a top-level "ts" field; conversations group by parent dir.
type MessagingDateFile string

// Path returns the file path as a string.
func (d MessagingDateFile) Path() string { return string(d) }
func (MessagingDateFile) logFile()       {}
func (MessagingDateFile) dataFile()      {}

// EmailDateFile is a path to a daily JSONL file under a Gmail account:
// <root>/gws/<acct>/gmail/YYYY-MM-DD.jsonl.
// Lines carry a top-level "ts" field; the account is the conversation unit.
type EmailDateFile string

// Path returns the file path as a string.
func (d EmailDateFile) Path() string { return string(d) }
func (EmailDateFile) logFile()       {}
func (EmailDateFile) dataFile()      {}

// CalendarDateFile is a path to a daily JSONL file under a single calendar:
// <root>/gws/<acct>/gcalendar/<calID>/YYYY-MM-DD.jsonl.
// Lines carry "updated" and "created" fields; the calendar id is the
// conversation unit.
type CalendarDateFile string

// Path returns the file path as a string.
func (d CalendarDateFile) Path() string { return string(d) }
func (CalendarDateFile) logFile()       {}
func (CalendarDateFile) dataFile()      {}

// ThreadFile is a path to a thread's JSONL file.
type ThreadFile string

// Path returns the file path as a string.
func (t ThreadFile) Path() string { return string(t) }
func (ThreadFile) logFile()       {}
func (ThreadFile) dataFile()      {}

// CommentsFile is a path to a Drive file's comments JSONL.
type CommentsFile string

// Path returns the file path as a string.
func (c CommentsFile) Path() string { return string(c) }
func (CommentsFile) logFile()       {}
func (CommentsFile) dataFile()      {}

// ConvMetaFile is a path to a conversation's .meta.json sidecar (messaging data).
// Drive file metadata uses DriveMetaFile (see paths/gws.go) which carries the
// modification date in the filename and needs separate dir/name handling.
type ConvMetaFile string

// Path returns the file path as a string.
func (m ConvMetaFile) Path() string { return string(m) }
func (ConvMetaFile) dataFile()      {}

// TabFile is a path to a document tab's markdown content.
type TabFile string

// Path returns the file path as a string.
func (t TabFile) Path() string { return string(t) }
func (TabFile) contentFile()   {}
func (TabFile) dataFile()      {}

// SheetFile is a path to a sheet's CSV export.
type SheetFile string

// Path returns the file path as a string.
func (s SheetFile) Path() string { return string(s) }
func (SheetFile) contentFile()   {}
func (SheetFile) dataFile()      {}

// FormulaFile is a path to a sheet's formulas CSV export.
type FormulaFile string

// Path returns the file path as a string.
func (f FormulaFile) Path() string { return string(f) }
func (FormulaFile) contentFile()   {}
func (FormulaFile) dataFile()      {}

// AttachmentFile is a path to an inline image or attachment.
type AttachmentFile string

// Path returns the file path as a string.
func (a AttachmentFile) Path() string { return string(a) }
func (AttachmentFile) dataFile()      {}

// MaintenanceFile is a path to an account's .maintenance.json state sidecar.
type MaintenanceFile string

// Path returns the file path as a string.
func (m MaintenanceFile) Path() string { return string(m) }
func (MaintenanceFile) dataFile()      {}

// SyncCursorsFile is a path to an account's .sync-cursors.yaml file.
type SyncCursorsFile string

// Path returns the file path as a string.
func (s SyncCursorsFile) Path() string { return string(s) }
func (SyncCursorsFile) dataFile()      {}

// PollMetricsFile is a path to an account's .poll-metrics.jsonl operational
// log, appended one record per poll per service.
type PollMetricsFile string

// Path returns the file path as a string.
func (p PollMetricsFile) Path() string { return string(p) }
func (PollMetricsFile) dataFile()      {}

// PendingDeletesFile is a path to a Gmail account's .pending-email-deletes
// queue file, written one email ID per line by the poller and drained during
// maintenance.
type PendingDeletesFile string

// Path returns the file path as a string.
func (p PendingDeletesFile) Path() string { return string(p) }
func (PendingDeletesFile) dataFile()      {}

// WorkstreamStoreDir is a path to a workspace's persistent workstream store
// directory: <base>/.workspaces/<name>/workstream/.
type WorkstreamStoreDir string

// Path returns the directory path as a string.
func (w WorkstreamStoreDir) Path() string { return string(w) }
