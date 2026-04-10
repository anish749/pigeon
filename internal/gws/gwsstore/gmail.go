package gwsstore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// DeleteEmail removes the email with the given ID from gmail date files.
// Scans date files newest-first (deletes are usually for recent emails)
// and rewrites the first file containing the email without its line.
// If the file becomes empty after removal, it is deleted.
//
// Unparseable lines in the file are preserved verbatim — only the
// matching email line is dropped.
//
// Returns nil without error if no file contains the email — the email
// may never have been synced, or may already have been deleted.
func DeleteEmail(gmailDir paths.GmailDir, emailID string) error {
	entries, err := os.ReadDir(gmailDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read gmail dir %s: %w", gmailDir.Path(), err)
	}

	var dateFiles []string
	for _, e := range entries {
		if e.IsDir() || !paths.IsDateFile(e.Name()) {
			continue
		}
		dateFiles = append(dateFiles, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dateFiles)))

	for _, name := range dateFiles {
		fullPath := filepath.Join(gmailDir.Path(), name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", fullPath, err)
		}

		filtered, found := dropEmailLine(data, emailID)
		if !found {
			continue
		}
		if len(filtered) == 0 {
			if err := os.Remove(fullPath); err != nil {
				return fmt.Errorf("remove empty date file %s: %w", fullPath, err)
			}
			return nil
		}
		if err := os.WriteFile(fullPath, filtered, 0o644); err != nil {
			return fmt.Errorf("rewrite %s: %w", fullPath, err)
		}
		return nil
	}

	return nil
}

// dropEmailLine rewrites JSONL bytes with any email line matching emailID
// removed. Non-matching lines and unparseable lines are preserved verbatim.
func dropEmailLine(data []byte, emailID string) ([]byte, bool) {
	found := false
	var out bytes.Buffer
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		if line, err := model.Parse(string(raw)); err == nil {
			if line.Type == "email" && line.Email != nil && line.Email.ID == emailID {
				found = true
				continue
			}
		}
		out.Write(raw)
		out.WriteByte('\n')
	}
	return out.Bytes(), found
}
