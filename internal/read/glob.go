package read

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// GlobFiles returns absolute paths to files under dir matching the given glob
// patterns, sorted by modification time (most recent first).
// Returns nil (not an error) when no files match.
func GlobFiles(dir string, globs []string) ([]string, error) {
	return rgFiles(dir, globs)
}

// Glob returns absolute paths to data files under dir, sorted by modification
// time (most recent first).
//
// Files covered:
//   - Messaging JSONL date files and thread files (slack, whatsapp).
//   - Gmail and Calendar JSONL date files.
//   - Drive content files (.md, .csv, comments.jsonl) — discovered via
//     their sibling drive-meta-YYYY-MM-DD.json files.
//   - Linear issue files — discovered by content (rg -l for "updatedAt" /
//     "createdAt" prefixes within the window), since they are named by
//     identifier rather than date.
//
// When since is zero, all data files are returned.
// When since is set:
//   - Date-named JSONL files are filtered by filename.
//   - Drive content files are included when their sibling drive-meta file's
//     date falls within the window.
//   - Thread files are filtered by content (rg -l for timestamp prefixes
//     within the window).
//   - Linear issue files are filtered by content (rg -l for updatedAt/
//     createdAt prefixes within the window).
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
	metaGlobs := paths.DriveMetaFileGlobsSince(since)
	if len(dateFileGlobs) == 0 && len(metaGlobs) == 0 {
		return nil, nil
	}

	allGlobs := append([]string{}, dateFileGlobs...)
	allGlobs = append(allGlobs, metaGlobs...)

	matched, err := rgFiles(dir, allGlobs)
	if err != nil {
		return nil, err
	}

	// Separate drive-meta matches from other date files so we can resolve
	// the metas into their parent DriveFileDir and enumerate sibling content.
	// Drive meta files themselves are not returned — only the content they
	// describe. ParseDriveMetaPath is three-valued: a false `ok` means the
	// path isn't drive-meta shaped (treated as a regular date file); a
	// non-nil error means the shape matched but the date was malformed —
	// we log and skip.
	var dateFiles []string
	var driveMetas []paths.DriveMetaFile
	for _, f := range matched {
		meta, ok, err := paths.ParseDriveMetaPath(f)
		if err != nil {
			slog.Error("parse drive-meta file", "path", f, "err", err)
			continue
		}
		if !ok {
			dateFiles = append(dateFiles, f)
			continue
		}
		driveMetas = append(driveMetas, meta)
	}
	reverseStrings(dateFiles)

	driveContent, err := expandDriveMetaMatches(driveMetas)
	if err != nil {
		return nil, err
	}

	// Find thread files containing messages within the window.
	threadFiles, err := rgFilesWithContent(dir, paths.ThreadGlobRg, threadDatePatterns(since))
	if err != nil {
		return nil, err
	}

	// Find Linear issue files updated or commented within the window. Their
	// filenames are identifier-based (e.g. PROJ-123.jsonl) so they cannot be
	// matched by date-filename globs and need a content-based search.
	linearFiles, err := rgFilesWithContent(dir, paths.LinearIssueGlobRg, linearDatePatterns(since))
	if err != nil {
		return nil, err
	}

	result := append([]string{}, dateFiles...)
	result = append(result, driveContent...)
	result = append(result, threadFiles...)
	result = append(result, linearFiles...)
	return result, nil
}

// expandDriveMetaMatches resolves each drive-meta file into the content
// files it describes. The meta file is the anchor of identity for a Drive
// file at a specific modification state, and ContentFiles enumerates the
// tabs, sheets, and comments at that snapshot.
func expandDriveMetaMatches(metas []paths.DriveMetaFile) ([]string, error) {
	var content []string
	var errs []error
	for _, meta := range metas {
		files, err := meta.ContentFiles()
		if err != nil {
			errs = append(errs, fmt.Errorf("list drive content for %s: %w", meta.Path(), err))
			continue
		}
		content = append(content, files...)
	}
	return content, errors.Join(errs...)
}
