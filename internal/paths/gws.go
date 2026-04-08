package paths

import "path/filepath"

// GWS directory and file naming constants.
const (
	gmailSubdir      = "gmail"
	gcalendarSubdir  = "gcalendar"
	gdriveSubdir     = "gdrive"
	attachSubdir     = "attachments"
	commentsFile     = "comments"
	metaFile         = "meta.json"
	markdownExt      = ".md"
	csvExt           = ".csv"
	formulaCSVSuffix = ".formulas.csv"
)

// GWS path types extend AccountDir for Google Workspace services.
//
//	AccountDir → GmailDir
//	AccountDir → CalendarDir
//	AccountDir → DriveDir → DriveFileDir

// Gmail returns a GmailDir for this account.
func (a AccountDir) Gmail() GmailDir {
	return GmailDir{account: a}
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

// MetaFile returns the path to the file's metadata.
func (f DriveFileDir) MetaFile() MetaFile {
	return MetaFile(filepath.Join(f.Path(), metaFile))
}

// CommentsFile returns the path to the file's comments JSONL.
func (f DriveFileDir) CommentsFile() CommentsFile {
	return CommentsFile(filepath.Join(f.Path(), commentsFile+FileExt))
}

// TabFile returns the path to a document tab's markdown content.
func (f DriveFileDir) TabFile(tabTitle string) string {
	return filepath.Join(f.Path(), tabTitle+markdownExt)
}

// SheetFile returns the path to a sheet's CSV export.
func (f DriveFileDir) SheetFile(sheetName string) string {
	return filepath.Join(f.Path(), sheetName+csvExt)
}

// FormulaFile returns the path to a sheet's formulas CSV export.
func (f DriveFileDir) FormulaFile(sheetName string) string {
	return filepath.Join(f.Path(), sheetName+formulaCSVSuffix)
}

// AttachmentFile returns the path to an inline image or attachment.
func (f DriveFileDir) AttachmentFile(filename string) string {
	return filepath.Join(f.Path(), attachSubdir, filename)
}
