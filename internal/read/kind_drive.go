package read

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// driveContentKind matches files inside a Drive file directory:
//
//	<root>/gws/<account>/gdrive/<slug>/Notes.md
//	<root>/gws/<account>/gdrive/<slug>/Sheet1.csv
//	<root>/gws/<account>/gdrive/<slug>/comments.jsonl
//
// These files are not self-describing; their authoritative modification date
// is encoded in the filename of the sibling drive-meta-YYYY-MM-DD.json. The
// meta file is the anchor of identity for a Drive file at a specific
// modification state, so every content file in the same directory shares its
// date.
type driveContentKind struct{}

func (driveContentKind) Name() string { return "drive-content" }

func (driveContentKind) Match(path string) bool {
	if !pathHasSegment(path, paths.GdriveSubdir) {
		return false
	}
	ext := filepath.Ext(path)
	return slices.Contains(paths.DriveContentExts, ext)
}

func (driveContentKind) LatestTs(path string) (time.Time, error) {
	return latestDriveMetaDate(filepath.Dir(path))
}

// latestDriveMetaDate finds the newest drive-meta-YYYY-MM-DD.json in dir and
// returns the date encoded in its filename. Returns the zero time if no meta
// file is present (a content file without a sibling meta is possible during
// partial syncs; treat it as "no known activity time" rather than an error).
func latestDriveMetaDate(dir string) (time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, fmt.Errorf("read drive dir %s: %w", dir, err)
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "drive-meta-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		meta, ok, err := paths.ParseDriveMetaPath(filepath.Join(dir, name))
		if err != nil {
			return time.Time{}, fmt.Errorf("parse drive-meta %s: %w", name, err)
		}
		if !ok {
			continue
		}
		d := meta.Date()
		if d.After(latest) {
			latest = d
		}
	}
	return latest, nil
}
