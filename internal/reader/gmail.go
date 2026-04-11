package reader

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// GmailResult holds the output of reading Gmail data.
type GmailResult struct {
	Emails []modelv1.EmailLine
}

// ReadGmail reads email data from a GmailDir, applying dedup, delete
// reconciliation, sorting, and filters.
//
// Algorithm (from read-protocol.md):
//  1. Collect all email lines from JSONL date files in the requested range.
//  2. Deduplicate by id (keep last occurrence).
//  3. Apply deletes: exclude emails with a matching email-delete line.
//  4. Sort by timestamp.
//  5. Apply --last=N if specified.
func ReadGmail(dir paths.GmailDir, filters Filters) (*GmailResult, error) {
	dateFiles, err := listSortedJSONL(dir.Path())
	if err != nil {
		return nil, fmt.Errorf("list gmail date files: %w", err)
	}

	selected := selectDateFiles(dir.Path(), dateFiles, filters)
	if len(selected) == 0 {
		return &GmailResult{}, nil
	}

	// Parse all emails from selected files. Collect parse errors so the
	// caller knows about partial failures.
	var allEmails []modelv1.EmailLine
	var errs []error
	for _, f := range selected {
		emails, err := parseEmailFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		allEmails = append(allEmails, emails...)
	}

	// Dedup by ID (keep last occurrence).
	allEmails = dedupEmails(allEmails)

	// Apply pending deletes.
	deleteIDs, err := loadPendingDeletes(dir.PendingDeletesPath())
	if err != nil {
		return nil, fmt.Errorf("load pending deletes: %w", err)
	}
	if len(deleteIDs) > 0 {
		allEmails = applyEmailDeletes(allEmails, deleteIDs)
	}

	// Sort by timestamp.
	sort.SliceStable(allEmails, func(i, j int) bool {
		return allEmails[i].Ts.Before(allEmails[j].Ts)
	})

	// Apply --since precise cutoff.
	if filters.Since > 0 {
		cutoff := time.Now().Add(-filters.Since)
		var filtered []modelv1.EmailLine
		for _, e := range allEmails {
			if !e.Ts.Before(cutoff) {
				filtered = append(filtered, e)
			}
		}
		allEmails = filtered
	}

	// Apply --last=N.
	last := filters.Last
	if last == 0 && filters.Since == 0 && filters.Date == "" {
		last = DefaultLast
	}
	if last > 0 && len(allEmails) > last {
		allEmails = allEmails[len(allEmails)-last:]
	}

	return &GmailResult{Emails: allEmails}, errors.Join(errs...)
}

// parseEmailFile reads a JSONL file and returns all email lines. Parse
// errors for individual lines are collected and returned alongside
// successfully parsed emails.
func parseEmailFile(path string) ([]modelv1.EmailLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var emails []modelv1.EmailLine
	var errs []error
	for _, rawLine := range splitLines(data) {
		line, err := modelv1.Parse(rawLine)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse line in %s: %w", filepath.Base(path), err))
			continue
		}
		if line.Type == modelv1.LineEmail && line.Email != nil {
			emails = append(emails, *line.Email)
		}
	}
	return emails, errors.Join(errs...)
}

// dedupEmails deduplicates emails by ID, keeping the last occurrence.
func dedupEmails(emails []modelv1.EmailLine) []modelv1.EmailLine {
	lastIndex := make(map[string]int, len(emails))
	for i, e := range emails {
		lastIndex[e.ID] = i
	}
	var result []modelv1.EmailLine
	for i, e := range emails {
		if lastIndex[e.ID] == i {
			result = append(result, e)
		}
	}
	return result
}

// loadPendingDeletes reads the pending email deletes file and returns
// a set of message IDs to exclude. Returns nil, nil when the file does
// not exist (no pending deletes is a normal state).
func loadPendingDeletes(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open pending deletes %s: %w", path, err)
	}
	defer f.Close()

	ids := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		id := strings.TrimSpace(scanner.Text())
		if id != "" {
			ids[id] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pending deletes %s: %w", path, err)
	}
	return ids, nil
}

// applyEmailDeletes removes emails whose IDs are in the delete set.
func applyEmailDeletes(emails []modelv1.EmailLine, deleteIDs map[string]bool) []modelv1.EmailLine {
	var result []modelv1.EmailLine
	for _, e := range emails {
		if !deleteIDs[e.ID] {
			result = append(result, e)
		}
	}
	return result
}

// --- shared helpers ---

// listSortedJSONL returns sorted JSONL file paths in a directory.
// Returns nil, nil when the directory does not exist.
func listSortedJSONL(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) == paths.FileExt {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files) // YYYY-MM-DD.jsonl sorts chronologically
	return files, nil
}

// selectDateFiles picks which date files to read based on filters.
func selectDateFiles(dir string, files []string, filters Filters) []string {
	switch {
	case filters.Date != "":
		target := filepath.Join(dir, filters.Date+paths.FileExt)
		for _, f := range files {
			if f == target {
				return []string{f}
			}
		}
		return nil
	case filters.Since > 0:
		cutoffDate := time.Now().Add(-filters.Since).Truncate(24 * time.Hour).Format("2006-01-02")
		cutoffPath := filepath.Join(dir, cutoffDate+paths.FileExt)
		i := sort.SearchStrings(files, cutoffPath)
		return files[i:]
	default:
		// No time filter — read all files (--last=N or default applies later).
		return files
	}
}

// splitLines splits bytes into non-empty lines.
func splitLines(data []byte) []string {
	s := string(data)
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
