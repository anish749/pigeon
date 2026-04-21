package read

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Kind is a recognised data file shape under the pigeon data tree. Each kind
// owns its own path detection and its own "when was this last active" logic,
// so callers never re-dispatch on extension, parent directory, or JSONL
// schema.
//
// Adding a new source (e.g. Jira, Linear) means adding a new Kind and
// registering it in `kinds` — no caller changes.
type Kind interface {
	// Name is a short identifier for logging/debugging.
	Name() string
	// Match reports whether this kind owns the given path.
	Match(path string) bool
	// LatestTs returns the most recent activity timestamp recorded in the
	// file. Implementations return the zero time when the kind has no
	// meaningful "latest activity" signal (e.g. calendar events, whose
	// "updated" field is not yet surfaced here).
	LatestTs(path string) (time.Time, error)
}

// kinds is the ordered list of recognised file shapes. Ordering matters:
// the first matching kind wins. More specific matchers (e.g. thread files,
// which sit under a specific parent dir) come before more general ones
// (e.g. generic date files).
var kinds = []Kind{
	threadFileKind{},
	driveContentKind{},
	emailDateKind{},
	calendarDateKind{},
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

// scanTsField walks a JSONL file line by line and returns the latest "ts"
// field value. Lines that don't parse or lack a "ts" field are silently
// skipped — the scan is deliberately narrow so it works across every JSONL
// schema in the tree that uses "ts" (slack, whatsapp, gmail) and tolerates
// schemas that don't (calendar events, drive comments).
func scanTsField(path string) (time.Time, error) {
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
		var rec struct {
			Ts time.Time `json:"ts"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Ts.After(latest) {
			latest = rec.Ts
		}
	}
	if err := scanner.Err(); err != nil {
		return time.Time{}, fmt.Errorf("scan %s: %w", path, err)
	}
	return latest, nil
}

// pathHasSegment reports whether any separator-delimited segment of path
// equals seg. Used by kind Match implementations to route GWS service paths
// (gmail, gcalendar, gdrive) to the right kind.
func pathHasSegment(path, seg string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(sep+path+sep, sep+seg+sep)
}
