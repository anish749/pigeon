package paths

import "path/filepath"

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
	_ LogFile =DateFile("")
	_ LogFile =ThreadFile("")
	_ LogFile =CommentsFile("")
	_ ContentFile = TabFile("")
	_ ContentFile = SheetFile("")
	_ ContentFile = FormulaFile("")
)

// DateFile is a path to a daily JSONL file (gmail or calendar).
type DateFile string

// Path returns the file path as a string.
func (d DateFile) Path() string { return string(d) }
func (DateFile) logFile()      {}

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

// MetaFile is a path to a document's metadata JSON file. It carries the
// directory and filename separately so callers can access the parent directory
// (e.g. for cleanup of sibling files) without re-parsing the path.
type MetaFile struct {
	dir  string
	name string
}

// NewMetaFile constructs a MetaFile from a directory and a filename.
// Constructors (ConversationDir.MetaFile, DriveFileDir.MetaFile) already
// have both pieces, so this avoids re-parsing via filepath.Dir.
func NewMetaFile(dir, name string) MetaFile {
	return MetaFile{dir: dir, name: name}
}

// Path returns the full file path.
func (m MetaFile) Path() string { return filepath.Join(m.dir, m.name) }

// Dir returns the directory containing this meta file.
func (m MetaFile) Dir() string { return m.dir }

// Name returns the filename (without the directory).
func (m MetaFile) Name() string { return m.name }

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
