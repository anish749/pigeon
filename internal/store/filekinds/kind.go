// Package filekinds classifies files under pigeon's data tree by storage
// shape and answers questions about them. Each Kind owns its own path
// detection and its own "when was this last active" logic, so callers never
// re-dispatch on extension, parent directory, or JSONL schema.
//
// Adding a new source (e.g. Jira, GitHub) means adding a new Kind and
// registering it in `kinds` — no caller changes.
package filekinds

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Kind is a recognised data file shape.
type Kind interface {
	// Name is a short identifier for logging/debugging.
	Name() string
	// Match reports whether this kind owns the given path.
	Match(path string) bool
	// LatestTs returns the most recent activity timestamp recorded in the
	// file, or the zero time when the kind has no meaningful signal.
	LatestTs(path string) (time.Time, error)
}

// kinds is the ordered list of recognised file shapes. Ordering matters:
// the first matching kind wins. More specific matchers (thread files,
// Drive content, GWS service date files, Linear issues) come before the
// general messaging date matcher, which is the catch-all for date-named
// JSONL files outside GWS/Linear.
var kinds = []Kind{
	threadFileKind{},
	driveContentKind{},
	emailDateKind{},
	calendarDateKind{},
	linearIssueKind{},
	messagingDateKind{},
}

// LatestTs dispatches to the registered Kind that owns the path and returns
// its latest-activity timestamp. Unrecognised files return the zero time and
// no error — the caller treats that as "no contribution to latest activity"
// rather than a failure.
func LatestTs(path string) (time.Time, error) {
	for _, k := range kinds {
		if k.Match(path) {
			return k.LatestTs(path)
		}
	}
	return time.Time{}, nil
}

// scanLatestTs walks a JSONL file line by line and returns the latest
// timestamp found in any of the named top-level fields. Lines that don't
// parse as JSON are silently skipped; lines without any of the fields
// contribute nothing.
//
// Kinds declare which fields carry their temporal signal:
//
//   - slack/whatsapp/gmail lines carry "ts"
//   - calendar events carry "updated" (and "created")
//   - linear issues carry "updatedAt"; linear comments carry "createdAt"
func scanLatestTs(path string, fields ...string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Email HTML bodies can be large; bump the per-line buffer well past
	// the 64KB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var latest time.Time
	for scanner.Scan() {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}
		for _, field := range fields {
			val, ok := raw[field]
			if !ok {
				continue
			}
			var t time.Time
			if err := json.Unmarshal(val, &t); err != nil {
				continue
			}
			if t.After(latest) {
				latest = t
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Time{}, fmt.Errorf("scan %s: %w", path, err)
	}
	return latest, nil
}

// pathHasSegment reports whether any separator-delimited segment of path
// equals seg.
func pathHasSegment(path, seg string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(sep+path+sep, sep+seg+sep)
}
