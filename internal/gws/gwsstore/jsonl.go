package gwsstore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anish749/pigeon/internal/gws/model"
	"github.com/anish749/pigeon/internal/paths"
)

// AppendLine appends a single JSONL line to a data file. Creates the file and
// parent directories if they don't exist. Appends a newline after the JSON.
func AppendLine(df paths.DataFile, line model.Line) error {
	path := df.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}

	data, err := model.Marshal(line)
	if err != nil {
		return fmt.Errorf("marshal line: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write to %s: %w", path, err)
	}
	return nil
}

// ReadLines reads all JSONL lines from a file. Returns nil, nil if the
// file doesn't exist. Unparseable lines are collected into the error
// but successfully parsed lines are still returned.
func ReadLines(path string) ([]model.Line, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var lines []model.Line
	var errs []error
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if text == "" {
			continue
		}
		l, err := model.Parse(text)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", lineNum, err))
			continue
		}
		lines = append(lines, l)
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("scan %s: %w", path, err))
	}
	return lines, errors.Join(errs...)
}

// Dedup removes duplicate lines by ID (keep last occurrence).
// Also applies delete semantics: email-delete removes the matching email.
// Lines without IDs (if any) are kept as-is.
func Dedup(lines []model.Line) []model.Line {
	// First pass: find the last occurrence index of each ID, and collect
	// deleted email IDs.
	lastIndex := make(map[string]int)
	deletedIDs := make(map[string]bool)

	for i, l := range lines {
		id := l.LineID()
		if id != "" {
			lastIndex[id] = i
		}
		if l.Type == "email-delete" {
			deletedIDs[l.EmailDelete.ID] = true
		}
	}

	// Second pass: keep only last-occurrence lines, excluding deleted emails
	// and the delete markers themselves.
	var result []model.Line
	for i, l := range lines {
		id := l.LineID()

		// Lines without IDs are always kept.
		if id == "" {
			result = append(result, l)
			continue
		}

		// Skip if not the last occurrence of this ID.
		if lastIndex[id] != i {
			continue
		}

		// Skip email-delete lines (they are consumed by the delete logic).
		if l.Type == "email-delete" {
			continue
		}

		// Skip emails that have been deleted.
		if l.Type == "email" && deletedIDs[id] {
			continue
		}

		result = append(result, l)
	}
	return result
}
