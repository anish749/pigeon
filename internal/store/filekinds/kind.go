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
	// Conversation returns the conversation identity that owns the file —
	// the grouping unit for `list --since` output. Messaging conversations
	// group many date and thread files under one directory; Drive docs,
	// calendars, and Linear issues each stand alone. Root is the data root
	// so the kind can compute a clean display label relative to it.
	Conversation(path, root string) Conversation
}

// Conversation is the identity of a source that extractConversations groups
// files under. Dir is what gets printed on the second line of `list --since`
// output; Display is the first-line label.
type Conversation struct {
	// Dir is the absolute directory or file that uniquely identifies the
	// conversation. For messaging this is the conversation directory; for
	// Linear it is the issue file itself (since the issue lives in one
	// JSONL file, not a subtree).
	Dir string
	// Display is a short human-readable label, typically a relative path
	// from the data root.
	Display string
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

// For returns the Kind that owns the path, or nil if no kind matches.
func For(path string) Kind {
	for _, k := range kinds {
		if k.Match(path) {
			return k
		}
	}
	return nil
}

// LatestTs is a convenience wrapper that dispatches to the matching Kind.
// Unrecognised files return the zero time and no error — callers treat that
// as "no contribution to latest activity" rather than a failure.
func LatestTs(path string) (time.Time, error) {
	if k := For(path); k != nil {
		return k.LatestTs(path)
	}
	return time.Time{}, nil
}

// scanLatestTs walks a JSONL file line by line and returns the latest
// timestamp found in any of the named top-level fields.
//
// Corruption surfaces: a line that fails to parse as JSON, or a matched
// field that fails to parse as a timestamp, returns an error with context.
// This keeps pigeon's JSONL stores fail-loud on real corruption rather than
// degrading to a silent stale timestamp.
//
// Missing fields are tolerated: the `{"type":"separator"}` line carries no
// ts, and thread files mix msg/react/edit lines that may or may not have a
// given field. Each kind declares every field name it might encounter; a
// line that has none of them is fine.
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
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			return time.Time{}, fmt.Errorf("parse line %d in %s: %w", lineNum, path, err)
		}
		for _, field := range fields {
			val, ok := raw[field]
			if !ok {
				continue
			}
			var t time.Time
			if err := json.Unmarshal(val, &t); err != nil {
				return time.Time{}, fmt.Errorf("parse %q field on line %d in %s: %w", field, lineNum, path, err)
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

// relativeConv builds a Conversation whose Dir is the absolute convDir and
// whose Display is convDir relative to the data root. Falls back to the
// absolute path if Rel fails (e.g. different volumes). Used by kinds whose
// conversation identity is a directory.
func relativeConv(convDir, root string) Conversation {
	display, err := filepath.Rel(root, convDir)
	if err != nil {
		display = convDir
	}
	return Conversation{Dir: convDir, Display: display}
}
