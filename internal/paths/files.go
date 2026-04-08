package paths

// DataFile is a sealed interface for typed file paths that hold JSONL data.
// The unexported method restricts implementations to this package.
type DataFile interface {
	Path() string
	private()
}

// Compile-time interface guards.
var (
	_ DataFile = DateFile("")
	_ DataFile = CommentsFile("")
	_ DataFile = MetaFile("")
)

// DateFile is a path to a daily JSONL file (gmail or calendar).
type DateFile string

// Path returns the file path as a string.
func (d DateFile) Path() string { return string(d) }

func (DateFile) private() {}

// CommentsFile is a path to a Drive file's comments JSONL.
type CommentsFile string

// Path returns the file path as a string.
func (c CommentsFile) Path() string { return string(c) }

func (CommentsFile) private() {}

// MetaFile is a path to a document's metadata JSON file.
type MetaFile string

// Path returns the file path as a string.
func (m MetaFile) Path() string { return string(m) }

func (MetaFile) private() {}
