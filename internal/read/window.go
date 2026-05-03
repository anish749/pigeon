package read

import (
	"errors"
	"fmt"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// WindowFiles discovers files under convDir whose contents could include a
// message with ts in [start, end]. Date files are matched by filename (the
// filename encodes the UTC date). Thread files are matched by content using
// rg -l for any in-window ts prefix.
//
// Thread file matching is coarse — a file with at least one in-window ts is
// included whole; callers must apply per-message ts filtering after parsing
// to honour the exact window.
func WindowFiles(convDir string, start, end time.Time) (dateFiles, threadFiles []string, err error) {
	if !end.After(start) {
		return nil, nil, fmt.Errorf("window: end (%s) must be after start (%s)", end, start)
	}

	dateGlobs := windowDateGlobs(start, end)
	threadPatterns := windowThreadPatterns(start, end)

	var errs []error
	dateFiles, err = rgFiles(convDir, dateGlobs)
	if err != nil {
		errs = append(errs, fmt.Errorf("date files: %w", err))
	}
	threadFiles, err = rgFilesWithContent(convDir, paths.ThreadGlobRg, threadPatterns)
	if err != nil {
		errs = append(errs, fmt.Errorf("thread files: %w", err))
	}
	return dateFiles, threadFiles, errors.Join(errs...)
}

// windowDateGlobs returns filename globs ("YYYY-MM-DD.jsonl") for every UTC
// date the [start, end] window touches.
func windowDateGlobs(start, end time.Time) []string {
	return walkUTCDates(start, end, func(d time.Time) string {
		return d.Format("2006-01-02") + paths.FileExt
	})
}

// windowThreadPatterns returns rg literal patterns matching the JSONL ts
// field for every UTC date the [start, end] window touches.
//
// Format depends on modelv1's serialization: `"ts":"YYYY-MM-DD`.
func windowThreadPatterns(start, end time.Time) []string {
	return walkUTCDates(start, end, func(d time.Time) string {
		return `"ts":"` + d.Format("2006-01-02")
	})
}

func walkUTCDates(start, end time.Time, fn func(time.Time) string) []string {
	s := start.UTC().Truncate(24 * time.Hour)
	e := end.UTC().Truncate(24 * time.Hour)
	var out []string
	for d := s; !d.After(e); d = d.Add(24 * time.Hour) {
		out = append(out, fn(d))
	}
	return out
}
