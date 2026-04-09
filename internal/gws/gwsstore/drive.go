package gwsstore

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anish749/pigeon/internal/paths"
)

// RemoveDriveFile deletes any local Drive file directories whose names
// match the given Drive fileID. Drive file directories are named either
// exactly "<fileID>" (empty-title fallback) or "<title-slug>-<fileID>",
// so a name matches if it equals fileID or ends with "-<fileID>".
//
// A missing gdrive directory is not an error — a newly-set-up account
// that hasn't backfilled yet has no local state to clean. Likewise a
// fileID with no matching directory returns nil.
//
// Used by the Drive poller when upstream reports a file as removed.
func RemoveDriveFile(drive paths.DriveDir, fileID string) error {
	entries, err := os.ReadDir(drive.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read drive dir %s: %w", drive.Path(), err)
	}
	suffix := "-" + fileID
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != fileID && !strings.HasSuffix(name, suffix) {
			continue
		}
		path := drive.File(name).Path()
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}
