package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
)

// readWindow is a half-open time range [start, end) used to scope a read.
// A zero-value window is unbounded — every message qualifies.
type readWindow struct {
	start, end time.Time
}

func (w readWindow) bounded() bool { return !w.start.IsZero() && !w.end.IsZero() }

func (w readWindow) contains(t time.Time) bool {
	if !w.bounded() {
		return true
	}
	return !t.Before(w.start) && t.Before(w.end)
}

// windowFromOpts derives a [start, end) window from ReadOpts.
//
//	--date X  → [X 00:00 UTC, X+1 00:00 UTC)
//	--since D → [now-D, now+1ns)
//	else      → unbounded
//
// The "+1ns" on --since's end is so the post-filter accepts messages whose
// ts equals "now"; readWindow.contains uses a half-open right edge.
func windowFromOpts(opts ReadOpts) (readWindow, error) {
	switch {
	case opts.Date != "":
		d, err := time.ParseInLocation("2006-01-02", opts.Date, time.UTC)
		if err != nil {
			return readWindow{}, fmt.Errorf("parse date %q: %w", opts.Date, err)
		}
		return readWindow{start: d, end: d.Add(24 * time.Hour)}, nil
	case opts.Since > 0:
		now := time.Now()
		return readWindow{start: now.Add(-opts.Since), end: now.Add(time.Nanosecond)}, nil
	}
	return readWindow{}, nil
}

// discoverFiles returns the date and thread files the read pipeline should
// open for a conversation under window w.
//
// Date files are filtered by filename: the directory layout encodes the UTC
// date as the filename ("YYYY-MM-DD.jsonl"), so an rg --files glob is a
// type-safe contract — no peeking at content.
//
// Thread files are listed whole. The contract for "is any member in window"
// lives at the model layer (typed time.Time on each parsed line). Pre-filtering
// by content patterns would couple to MsgLine.Ts's JSON serialization, which
// the user has rejected. The cost of reading every thread file matches the
// previous interleaveThreads behaviour and is bounded by the threads/
// directory size, not the window.
func (s *FSStore) discoverFiles(conv paths.ConversationDir, w readWindow) ([]paths.MessagingDateFile, []paths.ThreadFile, error) {
	dateFiles, err := discoverDateFiles(conv, w)
	if err != nil {
		return nil, nil, fmt.Errorf("discover date files: %w", err)
	}
	threadFiles, err := listThreadFiles(conv.ThreadsDir())
	if err != nil {
		return nil, nil, fmt.Errorf("discover thread files: %w", err)
	}
	return dateFiles, threadFiles, nil
}

func discoverDateFiles(conv paths.ConversationDir, w readWindow) ([]paths.MessagingDateFile, error) {
	var raw []string
	var err error
	if w.bounded() {
		raw, err = read.GlobFiles(conv.Path(), windowDateGlobs(w))
	} else {
		raw, err = listDateFiles(conv.Path())
	}
	if err != nil {
		return nil, err
	}
	out := make([]paths.MessagingDateFile, len(raw))
	for i, p := range raw {
		out[i] = paths.MessagingDateFile(p)
	}
	return out, nil
}

// windowDateGlobs returns one filename glob per UTC date the window touches.
// The filename pattern ("YYYY-MM-DD.jsonl") is a directory-layout contract —
// independent of any field's serialization — so it's safe for rg to match on.
func windowDateGlobs(w readWindow) []string {
	s := w.start.UTC().Truncate(24 * time.Hour)
	// Half-open right edge: subtract 1ns before truncating so we don't
	// pull in the day after a window that ends exactly at midnight.
	e := w.end.Add(-time.Nanosecond).UTC().Truncate(24 * time.Hour)
	var globs []string
	for d := s; !d.After(e); d = d.Add(24 * time.Hour) {
		globs = append(globs, d.Format("2006-01-02")+paths.FileExt)
	}
	return globs
}

func listThreadFiles(dir string) ([]paths.ThreadFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []paths.ThreadFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), paths.FileExt) {
			continue
		}
		files = append(files, paths.ThreadFile(filepath.Join(dir, e.Name())))
	}
	return files, nil
}
