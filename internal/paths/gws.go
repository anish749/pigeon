package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// GWS directory and file naming constants.
const (
	GmailSubdir         = "gmail"
	GcalendarSubdir     = "gcalendar"
	GdriveSubdir        = "gdrive"
	attachSubdir        = "attachments"
	commentsFile        = "comments"
	driveMetaFilePrefix = "drive-meta-"
	driveMetaFileExt    = ".json"
	formulaCSVSuffix    = ".formulas.csv"
	pollMetricsFile     = ".poll-metrics.jsonl"
	pendingDeletesFile  = ".pending-email-deletes"
)

// GWSServices lists the GWS service subdirectory names.
var GWSServices = []string{GmailSubdir, GcalendarSubdir, GdriveSubdir}

// Drive content file extensions. Drive file directories hold the exported
// content of a single Google Doc or Sheet: markdown per tab, CSV per sheet,
// and a JSONL comments snapshot.
const (
	MarkdownExt = ".md"
	CSVExt      = ".csv"
)

// DriveContentExts lists the file extensions of Drive content files — the
// files that live alongside a drive-meta-*.json in a Drive file directory.
// Used by the read layer to discover Drive content across glob patterns.
var DriveContentExts = []string{MarkdownExt, CSVExt, FileExt}

// DriveMetaFileGlob is the glob pattern for matching all Drive file metadata
// files in a Drive file directory. Used for cleanup (removing stale meta files
// when a file is re-synced) and read-layer discovery (finding files modified
// within a time window via filename).
const DriveMetaFileGlob = driveMetaFilePrefix + "*" + driveMetaFileExt

// ParseDriveMetaPath attempts to parse a raw filesystem path as a
// DriveMetaFile. Used by the read layer to convert ripgrep output into
// typed values. Three-valued result:
//
//   - (meta, true, nil): path is a valid drive-meta-YYYY-MM-DD.json file.
//   - (_, false, nil): path does not look like a drive-meta file at all
//     (wrong prefix or extension). Not an error — callers should treat
//     the path as unrelated and move on.
//   - (_, true, err): path has the drive-meta prefix and extension but
//     the date portion failed to parse. A real error — callers should
//     log this, since it means an unexpected filename shape.
func ParseDriveMetaPath(path string) (DriveMetaFile, bool, error) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, driveMetaFilePrefix) || !strings.HasSuffix(base, driveMetaFileExt) {
		return DriveMetaFile{}, false, nil
	}
	dateStr := strings.TrimSuffix(strings.TrimPrefix(base, driveMetaFilePrefix), driveMetaFileExt)
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		return DriveMetaFile{}, true, fmt.Errorf("invalid drive-meta date %q in %s: %w", dateStr, path, err)
	}
	return DriveMetaFile{
		dir:  filepath.Dir(path),
		name: base,
	}, true, nil
}

// DriveMetaFileGlobsSince returns ripgrep filename glob patterns matching
// drive-meta files with modification dates within the last `since` duration.
// One pattern per UTC day in the window. The read layer uses these to
// discover Drive files modified recently — matched meta files are then
// parsed via ParseDriveMetaPath and expanded with ContentFiles.
func DriveMetaFileGlobsSince(since time.Duration) []string {
	now := time.Now().UTC()
	cutoff := now.Add(-since).Truncate(24 * time.Hour)
	today := now.Truncate(24 * time.Hour)

	var globs []string
	for d := cutoff; !d.After(today); d = d.Add(24 * time.Hour) {
		globs = append(globs, driveMetaFilePrefix+d.Format("2006-01-02")+driveMetaFileExt)
	}
	return globs
}

// IsGWSFile reports whether the given file path lives under a Google Workspace
// or Linear subdirectory (gmail, gcalendar, gdrive, issues). Used by the
// maintenance pass to route GWS/Linear files to ID-based dedup instead of
// messaging compaction.
func IsGWSFile(path string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(path, sep+GmailSubdir+sep) ||
		strings.Contains(path, sep+GcalendarSubdir+sep) ||
		strings.Contains(path, sep+GdriveSubdir+sep) ||
		strings.Contains(path, sep+linearIssuesSubdir+sep)
}

// GWS path types extend AccountDir for Google Workspace services.
//
//	AccountDir → GmailDir
//	AccountDir → CalendarDir
//	AccountDir → DriveDir → DriveFileDir

// Gmail returns a GmailDir for this account.
func (a AccountDir) Gmail() GmailDir {
	return GmailDir{account: a}
}

// PollMetricsFile returns the path to the poll metrics JSONL file for this
// account. One line is appended per service per poll cycle — used to analyze
// poll hit-rate and latency for debouncer / adaptive-interval decisions.
func (a AccountDir) PollMetricsFile() PollMetricsFile {
	return PollMetricsFile(filepath.Join(a.Path(), pollMetricsFile))
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
	return filepath.Join(g.account.Path(), GmailSubdir)
}

// DateFile returns the path to a daily email file.
func (g GmailDir) DateFile(date string) EmailDateFile {
	return EmailDateFile(filepath.Join(g.Path(), date+FileExt))
}

// PendingDeletesFile returns the path to the pending email deletes file.
func (g GmailDir) PendingDeletesFile() PendingDeletesFile {
	return PendingDeletesFile(filepath.Join(g.Path(), pendingDeletesFile))
}

// CalendarDir represents a calendar directory: <account>/gcalendar/<calID>/
type CalendarDir struct {
	account AccountDir
	calID   string
}

// Path returns the calendar directory path.
func (c CalendarDir) Path() string {
	return filepath.Join(c.account.Path(), GcalendarSubdir, c.calID)
}

// DateFile returns the path to a daily events file.
func (c CalendarDir) DateFile(date string) CalendarDateFile {
	return CalendarDateFile(filepath.Join(c.Path(), date+FileExt))
}

// DriveDir represents the gdrive directory: <account>/gdrive/
type DriveDir struct {
	account AccountDir
}

// Path returns the gdrive directory path.
func (d DriveDir) Path() string {
	return filepath.Join(d.account.Path(), GdriveSubdir)
}

// File returns a DriveFileDir for the given slug.
func (d DriveDir) File(slug string) DriveFileDir {
	return DriveFileDir{drive: d, slug: slug}
}

// DriveFileDir represents a Drive file directory: <account>/gdrive/<slug>/.
// Only constructable through the type chain (DataRoot → ... → DriveDir.File).
// Read-layer callers that need to enumerate content for a discovered Drive
// file use DriveMetaFile.ContentFiles instead — the meta file is the anchor
// of identity for a Drive file at a specific modification state.
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
// Unlike conversation ConvMetaFile (a fixed .meta.json sidecar), Drive meta files
// are date-partitioned and require sibling file cleanup on update.
//
// A DriveMetaFile anchors the identity of a Drive file at a specific
// modification state: all content files (markdown tabs, CSV sheets, comments)
// in the same directory belong to that same snapshot. ContentFiles() returns
// those content files.
type DriveMetaFile struct {
	dir  string
	name string
}

// Path returns the full file path.
func (m DriveMetaFile) Path() string { return filepath.Join(m.dir, m.name) }
func (DriveMetaFile) dataFile()      {}

// Dir returns the directory containing this meta file.
func (m DriveMetaFile) Dir() string { return m.dir }

// Name returns the filename (without the directory).
func (m DriveMetaFile) Name() string { return m.name }

// ContentFiles returns absolute paths of the Drive content files (markdown
// tabs, CSV sheets, comments JSONL) that this meta file describes. The meta
// file is the anchor of identity for a Drive file at a specific modification
// date; all content files in the same directory belong to that same Drive
// file snapshot. Subdirectories (e.g. attachments/) and non-content files are
// skipped.
func (m DriveMetaFile) ContentFiles() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("read drive dir %s: %w", m.dir, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if slices.Contains(DriveContentExts, ext) {
			files = append(files, filepath.Join(m.dir, entry.Name()))
		}
	}
	return files, nil
}

// CommentsFile returns the path to the file's comments JSONL.
func (f DriveFileDir) CommentsFile() CommentsFile {
	return CommentsFile(filepath.Join(f.Path(), commentsFile+FileExt))
}

// TabFile returns the path to a document tab's markdown content.
func (f DriveFileDir) TabFile(tabTitle string) TabFile {
	return TabFile(filepath.Join(f.Path(), tabTitle+MarkdownExt))
}

// SheetFile returns the path to a sheet's CSV export.
func (f DriveFileDir) SheetFile(sheetName string) SheetFile {
	return SheetFile(filepath.Join(f.Path(), sheetName+CSVExt))
}

// FormulaFile returns the path to a sheet's formulas CSV export.
func (f DriveFileDir) FormulaFile(sheetName string) FormulaFile {
	return FormulaFile(filepath.Join(f.Path(), sheetName+formulaCSVSuffix))
}

// AttachmentFile returns the path to an inline image or attachment.
func (f DriveFileDir) AttachmentFile(filename string) AttachmentFile {
	return AttachmentFile(filepath.Join(f.Path(), attachSubdir, filename))
}
