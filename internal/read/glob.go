package read

import (
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// Glob returns absolute paths to data files under dir, sorted by modification
// time (most recent first).
//
// When since is zero, all .jsonl files are returned.
// When since is set:
//   - Date files are filtered by filename (only dates within the window).
//   - Thread files are filtered by content (rg -l for timestamp prefixes
//     within the window).
func Glob(dir string, since time.Duration) ([]string, error) {
	if since == 0 {
		files, err := rgFiles(dir, []string{"*" + paths.FileExt})
		if err != nil {
			return nil, err
		}
		reverseStrings(files) // most recent first
		return files, nil
	}

	globs := dateGlobs(since)
	if len(globs) == 0 {
		return nil, nil
	}

	dateFiles, err := rgFiles(dir, globs)
	if err != nil {
		return nil, err
	}
	reverseStrings(dateFiles)

	// Find thread files containing messages within the window.
	patterns := threadDatePatterns(since)
	threadFiles, err := rgFilesWithContent(dir, paths.ThreadGlobRg, patterns)
	if err != nil {
		return nil, err
	}

	return append(dateFiles, threadFiles...), nil
}
