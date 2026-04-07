// Package read provides file discovery (Glob) and content search (Grep)
// over pigeon's JSONL data tree using ripgrep.
package read

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// dateGlobs generates filename glob patterns for all UTC dates from
// (now - since) through today. Each pattern matches a date file like
// "2026-04-07.jsonl".
func dateGlobs(since time.Duration) []string {
	now := time.Now().UTC()
	cutoff := now.Add(-since).Truncate(24 * time.Hour)
	today := now.Truncate(24 * time.Hour)

	var globs []string
	for d := cutoff; !d.After(today); d = d.Add(24 * time.Hour) {
		globs = append(globs, d.Format("2006-01-02")+paths.FileExt)
	}
	return globs
}

// threadDatePatterns generates rg -e patterns that match timestamp prefixes
// inside thread files for dates within the since window. Each pattern is a
// literal string like `"ts":"2026-04-07`.
//
// This depends on the JSONL serialization format in modelv1: the ts field
// must be named "ts", use ISO 8601 dates starting with YYYY-MM-DD, and
// appear as "ts":"YYYY-MM-DD in the serialized JSON. If the model changes
// field names, timestamp format, or serialization order, this breaks.
func threadDatePatterns(since time.Duration) []string {
	now := time.Now().UTC()
	cutoff := now.Add(-since).Truncate(24 * time.Hour)
	today := now.Truncate(24 * time.Hour)

	var patterns []string
	for d := cutoff; !d.After(today); d = d.Add(24 * time.Hour) {
		patterns = append(patterns, `"ts":"`+d.Format("2006-01-02"))
	}
	return patterns
}

// rgFiles runs `rg --files` with the given glob patterns under dir and
// returns absolute file paths. Results are sorted by modification time,
// most recent first.
func rgFiles(dir string, globs []string) ([]string, error) {
	args := []string{"--files", "--sort=modified"}
	for _, g := range globs {
		args = append(args, "--glob", g)
	}
	args = append(args, dir)
	return runRg(args, dir)
}

// rgFilesWithContent runs `rg -l` (files-with-matches) to find files under
// dir matching the given glob that contain at least one of the patterns.
// Returns absolute file paths.
func rgFilesWithContent(dir string, glob string, patterns []string) ([]string, error) {
	args := []string{"-l", "--glob", glob}
	for _, p := range patterns {
		args = append(args, "-e", p)
	}
	args = append(args, dir)
	return runRg(args, dir)
}

// runRg executes rg with the given args and returns absolute file paths.
// Exit code 1 (no matches) returns nil, nil.
func runRg(args []string, dir string) ([]string, error) {
	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("rg: %w", err)
	}

	var files []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		p := string(line)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		files = append(files, p)
	}
	return files, nil
}

// reverseStrings reverses a string slice in place.
func reverseStrings(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

