package paths

import "path/filepath"

// GWS directory and file naming constants.
const (
	gmailSubdir         = "gmail"
	gcalendarSubdir     = "gcalendar"
	gdriveSubdir        = "gdrive"
	attachSubdir        = "attachments"
	commentsFile        = "comments"
	driveMetaFilePrefix = "drive-meta-"
	driveMetaFileExt    = ".json"
	markdownExt         = ".md"
	csvExt              = ".csv"
	formulaCSVSuffix    = ".formulas.csv"
	pollMetricsFile     = ".poll-metrics.jsonl"
)

// DriveMetaFileGlob is the glob pattern for matching all Drive file metadata
// files in a Drive file directory. Used for cleanup (removing stale meta files
// when a file is re-synced) and read-layer discovery (finding files modified
// within a time window via filename).
const DriveMetaFileGlob = driveMetaFilePrefix + "*" + driveMetaFileExt

// GWS path types extend AccountDir for Google Workspace services.
//
//	AccountDir → GmailDir
//	AccountDir → CalendarDir
//	AccountDir → DriveDir → DriveFileDir

// Gmail returns a GmailDir for this account.
func (a AccountDir) Gmail() GmailDir {
	return GmailDir{account: a}
}

// PollMetricsPath returns the path to the poll metrics JSONL file for this
// account. One line is appended per service per poll cycle — used to analyze
// poll hit-rate and latency for debouncer / adaptive-interval decisions.
func (a AccountDir) PollMetricsPath() string {
	return filepath.Join(a.Path(), pollMetricsFile)
}

// Calendar returns a CalendarDir for the given calendar ID.
func (a AccountDir) Calendar(calID string) CalendarDir {
	return CalendarDir{account: a, calID: calID}
}

// Drive returns a DriveDir for this account.
func (a AccountDir) Drive() DriveDir {
	return DriveDir{account: a}
}

// GmailDir represents the gmail directory: <account>/gmail/
type GmailDir struct {
	account AccountDir
}

// Path returns the gmail directory path.
func (g GmailDir) Path() string {
	return filepath.Join(g.account.Path(), gmailSubdir)
}

// DateFile returns the path to a daily email file.
func (g GmailDir) DateFile(date string) DateFile {
	return DateFile(filepath.Join(g.Path(), date+FileExt))
}

// CalendarDir represents a calendar directory: <account>/gcalendar/<calID>/
type CalendarDir struct {
	account AccountDir
	calID   string
}

// Path returns the calendar directory path.
func (c CalendarDir) Path() string {
	return filepath.Join(c.account.Path(), gcalendarSubdir, c.calID)
}

// DateFile returns the path to a daily events file.
func (c CalendarDir) DateFile(date string) DateFile {
	return DateFile(filepath.Join(c.Path(), date+FileExt))
}

// DriveDir represents the gdrive directory: <account>/gdrive/
type DriveDir struct {
	account AccountDir
}

// Path returns the gdrive directory path.
func (d DriveDir) Path() string {
	return filepath.Join(d.account.Path(), gdriveSubdir)
}

// File returns a DriveFileDir for the given slug.
func (d DriveDir) File(slug string) DriveFileDir {
	return DriveFileDir{drive: d, slug: slug}
}

// DriveFileDir represents a Drive file directory: <account>/gdrive/<slug>/
type DriveFileDir struct {
	drive DriveDir
	slug  string
}

// Path returns the drive file directory path.
func (f DriveFileDir) Path() string {
	return filepath.Join(f.drive.Path(), f.slug)
}

// MetaFile returns the path to the file's metadata, with the Drive
// modification date encoded in the filename (drive-meta-YYYY-MM-DD.json).
// The date enables filename-based filtering in the read layer without
// parsing the file contents.
func (f DriveFileDir) MetaFile(modifiedDate string) DriveMetaFile {
	return DriveMetaFile{
		dir:  f.Path(),
		name: driveMetaFilePrefix + modifiedDate + driveMetaFileExt,
	}
}

// DriveMetaFile is a path to a Google Drive file's metadata JSON, named
// drive-meta-YYYY-MM-DD.json where the date is the Drive modification date.
// Unlike conversation MetaFile (a fixed .meta.json sidecar), Drive meta files
// are date-partitioned and require sibling file cleanup on update. The struct
// carries dir and name separately so callers can access the parent directory
// directly rather than parsing the path via filepath.Dir.
type DriveMetaFile struct {
	dir  string
	name string
}


// Path returns the full file path.
func (m DriveMetaFile) Path() string { return filepath.Join(m.dir, m.name) }

// Dir returns the directory containing this meta file.
func (m DriveMetaFile) Dir() string { return m.dir }

// Name returns the filename (without the directory).
func (m DriveMetaFile) Name() string { return m.name }

// CommentsFile returns the path to the file's comments JSONL.
func (f DriveFileDir) CommentsFile() CommentsFile {
	return CommentsFile(filepath.Join(f.Path(), commentsFile+FileExt))
}

// TabFile returns the path to a document tab's markdown content.
func (f DriveFileDir) TabFile(tabTitle string) TabFile {
	return TabFile(filepath.Join(f.Path(), tabTitle+markdownExt))
}

// SheetFile returns the path to a sheet's CSV export.
func (f DriveFileDir) SheetFile(sheetName string) SheetFile {
	return SheetFile(filepath.Join(f.Path(), sheetName+csvExt))
}

// FormulaFile returns the path to a sheet's formulas CSV export.
func (f DriveFileDir) FormulaFile(sheetName string) FormulaFile {
	return FormulaFile(filepath.Join(f.Path(), sheetName+formulaCSVSuffix))
}

// AttachmentFile returns the path to an inline image or attachment.
func (f DriveFileDir) AttachmentFile(filename string) AttachmentFile {
	return AttachmentFile(filepath.Join(f.Path(), attachSubdir, filename))
}
