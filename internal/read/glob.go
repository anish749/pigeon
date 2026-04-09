package read

import (
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// Glob returns absolute paths to data files under dir, sorted by modification
// time (most recent first).
//
// Files covered:
//   - Messaging JSONL date files and thread files (slack, whatsapp).
//   - Gmail and Calendar JSONL date files.
//   - Drive content files (.md, .csv, comments.jsonl) — discovered via
//     their sibling drive-meta-YYYY-MM-DD.json files.
//
// When since is zero, all data files are returned.
// When since is set:
//   - Date-named JSONL files are filtered by filename.
//   - Drive content files are included when their sibling drive-meta file's
//     date falls within the window.
//   - Thread files are filtered by content (rg -l for timestamp prefixes
//     within the window).
func Glob(dir string, since time.Duration) ([]string, error) {
	if since == 0 {
		globs := []string{"*" + paths.FileExt}
		for _, ext := range paths.DriveContentExts {
			if ext != paths.FileExt {
				globs = append(globs, "*"+ext)
			}
		}
		files, err := rgFiles(dir, globs)
		if err != nil {
			return nil, err
		}
		reverseStrings(files) // most recent first
		return files, nil
	}

	dateFileGlobs := dateGlobs(since)
	metaGlobs := driveMetaGlobs(since)
	if len(dateFileGlobs) == 0 && len(metaGlobs) == 0 {
		return nil, nil
	}

	allGlobs := append([]string{}, dateFileGlobs...)
	allGlobs = append(allGlobs, metaGlobs...)

	matched, err := rgFiles(dir, allGlobs)
	if err != nil {
		return nil, err
	}

	// Separate drive-meta matches from other date files so we can expand
	// the metas into their sibling content files. Drive meta files themselves
	// are not returned — only the content they point to.
	var dateFiles []string
	var driveMetas []string
	for _, f := range matched {
		if paths.IsDriveMetaFile(f) {
			driveMetas = append(driveMetas, f)
		} else {
			dateFiles = append(dateFiles, f)
		}
	}
	reverseStrings(dateFiles)

	driveContent, err := expandDriveContent(driveMetas)
	if err != nil {
		return nil, err
	}

	// Find thread files containing messages within the window.
	patterns := threadDatePatterns(since)
	threadFiles, err := rgFilesWithContent(dir, paths.ThreadGlobRg, patterns)
	if err != nil {
		return nil, err
	}

	result := append([]string{}, dateFiles...)
	result = append(result, driveContent...)
	result = append(result, threadFiles...)
	return result, nil
}
