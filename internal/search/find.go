package search

import (
	"bytes"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// FindFiles returns absolute paths to data files under dir that fall within
// the since window. Date files are filtered by filename (YYYY-MM-DD.jsonl);
// thread files are always included because their filenames are timestamps,
// not dates. When since is zero, all data files are returned.
//
// Uses rg --files for fast filesystem traversal.
func FindFiles(dir string, since time.Duration) ([]string, error) {
	globs, err := fileGlobs(dir, since)
	if err != nil {
		return nil, err
	}

	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return findFilesFallback(dir, globs)
	}

	args := []string{"--files"}
	for _, g := range globs {
		args = append(args, "--glob", g)
	}
	args = append(args, dir)

	out, err := exec.Command(rgPath, args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no files matched
		}
		return nil, fmt.Errorf("rg --files: %w", err)
	}

	var files []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		files = append(files, string(line))
	}
	return files, nil
}

// Grep runs a content search over data files under dir within the since
// window. Returns raw rg/grep output. Uses rg with a grep fallback.
func Grep(query, dir string, since time.Duration, context int) ([]byte, error) {
	globs, err := fileGlobs(dir, since)
	if err != nil {
		return nil, err
	}

	if rgPath, err := exec.LookPath("rg"); err == nil {
		return captureRg(rgPath, query, dir, globs, context)
	}
	return captureGrepFallback(query, dir, globs, context)
}

// fileGlobs returns rg --glob patterns that select data files within the
// since window. Date files are matched by filename; thread files are always
// included.
func fileGlobs(dir string, since time.Duration) ([]string, error) {
	if since == 0 {
		return []string{"*" + paths.FileExt}, nil
	}

	cutoff := time.Now().Add(-since).Truncate(24 * time.Hour)

	// Scan for date filenames in the tree. We need the set of unique
	// date filenames (e.g. "2026-04-07.jsonl") that fall within the window.
	seen := make(map[string]bool)
	var walkErr error
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			walkErr = err
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if len(name) != len("YYYY-MM-DD"+paths.FileExt) {
			return nil
		}
		dateStr := name[:10]
		t, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			return nil
		}
		if !t.Before(cutoff) {
			seen[name] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, walkErr)
	}

	var globs []string
	for name := range seen {
		globs = append(globs, name)
	}
	// Always include thread files — can't date-filter by filename.
	globs = append(globs, paths.ThreadGlobRg)
	return globs, nil
}


// --- rg / grep execution ---

func captureRg(rgPath, query, dir string, globs []string, context int) ([]byte, error) {
	args := []string{"--color=never"}
	for _, g := range globs {
		args = append(args, "--glob", g)
	}
	if context > 0 {
		args = append(args, fmt.Sprintf("-C%d", context))
	}
	args = append(args, query, dir)

	out, err := exec.Command(rgPath, args...).Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return nil, nil
	}
	return out, err
}

func captureGrepFallback(query, dir string, globs []string, context int) ([]byte, error) {
	var dateIncludes []string
	searchThreads := false
	for _, g := range globs {
		if g == paths.ThreadGlobRg {
			searchThreads = true
		} else {
			dateIncludes = append(dateIncludes, g)
		}
	}

	var out []byte

	if len(dateIncludes) > 0 {
		args := []string{"-r", "--color=never"}
		for _, inc := range dateIncludes {
			args = append(args, "--include", inc)
		}
		if context > 0 {
			args = append(args, fmt.Sprintf("-C%d", context))
		}
		args = append(args, query, dir)

		dateOut, err := exec.Command("grep", args...).Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// no matches, continue
			} else {
				return nil, err
			}
		}
		out = dateOut
	}

	if searchThreads {
		threadOut, err := grepThreadFiles(query, dir, context)
		if err != nil {
			return out, err
		}
		out = append(out, threadOut...)
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func grepThreadFiles(query, dir string, context int) ([]byte, error) {
	grepArgs := []string{"-H", "--color=never"}
	if context > 0 {
		grepArgs = append(grepArgs, fmt.Sprintf("-C%d", context))
	}
	grepArgs = append(grepArgs, query)

	args := []string{dir, "-path", paths.ThreadGlobFind, "-exec", "grep"}
	args = append(args, grepArgs...)
	args = append(args, "{}", "+")

	out, err := exec.Command("find", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// findFilesFallback uses Go's WalkDir when rg is not available.
func findFilesFallback(dir string, globs []string) ([]string, error) {
	// Build a set of exact filenames and check for thread glob.
	dateNames := make(map[string]bool)
	includeThreads := false
	for _, g := range globs {
		if g == paths.ThreadGlobRg {
			includeThreads = true
		} else if g == "*"+paths.FileExt {
			// All files — just walk everything.
			return findAllFiles(dir)
		} else {
			dateNames[g] = true
		}
	}

	var files []string
	var walkErr error
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			walkErr = err
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, paths.FileExt) {
			return nil
		}
		if dateNames[name] {
			files = append(files, path)
			return nil
		}
		if includeThreads && filepath.Base(filepath.Dir(path)) == paths.ThreadsSubdir {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, walkErr)
	}
	return files, nil
}

func findAllFiles(dir string) ([]string, error) {
	var files []string
	var walkErr error
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			walkErr = err
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), paths.FileExt) {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, walkErr)
	}
	return files, nil
}
