package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/timeutil"
	"github.com/anish749/pigeon/internal/workspace"
)

// RunListSince prints conversations with activity within the given duration
// window, scoped by the active workspace and optional platform/account.
// Each line is "<display>  last: <age> ago" followed by the conversation
// directory path on the next line.
func RunListSince(ws *workspace.Workspace, platform, account, since string) error {
	sinceDur, err := timeutil.ParseDuration(since)
	if err != nil {
		return fmt.Errorf("invalid --since value %q: %w", since, err)
	}

	dirs, err := read.SearchDirs(ws, platform, account)
	if err != nil {
		return err
	}

	var allFiles []paths.DataFile
	for _, dir := range dirs {
		files, err := read.Glob(dir, sinceDur)
		if err != nil {
			return err
		}
		allFiles = append(allFiles, files...)
	}
	if len(allFiles) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	root := paths.DefaultDataRoot().Path()
	convs, err := extractConversations(allFiles, root)
	if err != nil {
		return err
	}
	if len(convs) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	now := time.Now()
	for _, c := range convs {
		if !c.LatestTime.IsZero() {
			fmt.Printf("%s  last: %s ago\n", c.Display, timeutil.FormatAge(now.Sub(c.LatestTime)))
		} else {
			fmt.Printf("%s  active\n", c.Display)
		}
		fmt.Printf("  %s\n", c.Dir)
	}
	return nil
}

// activeConv represents a conversation discovered from file paths.
type activeConv struct {
	Display    string    // platform/account/conversation
	Dir        string    // absolute conversation directory
	LatestTime time.Time // most recent activity timestamp
}

// extractConversations deduplicates files into unique conversations,
// tracking the most recent activity timestamp per conversation. Today the
// grouping uses the legacy parts[0:3] heuristic — a follow-up commit
// replaces it with a paths.DataFile-aware dispatch so per-source granularity
// (per-doc Drive, per-issue Linear, per-calendar) is preserved.
func extractConversations(files []paths.DataFile, root string) ([]activeConv, error) {
	seen := make(map[string]*activeConv)
	var order []string
	for _, f := range files {
		path := f.Path()
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if _, isThread := f.(paths.ThreadFile); isThread {
			for i, p := range parts {
				if p == paths.ThreadsSubdir {
					parts = append(parts[:i], parts[i+1:]...)
					break
				}
			}
		}
		if len(parts) < 4 {
			continue
		}
		convDir := filepath.Join(root, parts[0], parts[1], parts[2])

		c, ok := seen[convDir]
		if !ok {
			c = &activeConv{
				Display: strings.Join(parts[:3], "/"),
				Dir:     convDir,
			}
			seen[convDir] = c
			order = append(order, convDir)
		}

		ts, err := LatestTs(f)
		if err != nil {
			return nil, fmt.Errorf("latest ts %s: %w", path, err)
		}
		if ts.After(c.LatestTime) {
			c.LatestTime = ts
		}
	}

	result := make([]activeConv, len(order))
	for i, key := range order {
		result[i] = *seen[key]
	}
	return result, nil
}

// LatestTs returns the most recent activity timestamp recorded by the file,
// dispatched on the typed paths.DataFile so each kind reads the right field
// (or, for Drive content, the date encoded in the sibling drive-meta name).
// Returns the zero time and no error for kinds whose values do not contribute
// a meaningful "latest activity" — sidecars, queues, identity, etc.
func LatestTs(f paths.DataFile) (time.Time, error) {
	switch v := f.(type) {
	case paths.MessagingDateFile:
		return scanLatestTs(v.Path(), "ts")
	case paths.EmailDateFile:
		return scanLatestTs(v.Path(), "ts")
	case paths.CalendarDateFile:
		return scanLatestTs(v.Path(), "updated", "created")
	case paths.ThreadFile:
		return scanLatestTs(v.Path(), "ts")
	case paths.CommentsFile:
		return scanLatestTs(v.Path(), "ts")
	case paths.IssueFile:
		return scanLatestTs(v.Path(), "updatedAt", "createdAt")
	case paths.TabFile:
		return latestDriveMetaDate(filepath.Dir(v.Path()))
	case paths.SheetFile:
		return latestDriveMetaDate(filepath.Dir(v.Path()))
	case paths.FormulaFile:
		return latestDriveMetaDate(filepath.Dir(v.Path()))
	case paths.DriveMetaFile:
		return v.Date(), nil
	}
	// AttachmentFile, ConvMetaFile, PeopleFile, MaintenanceFile,
	// SyncCursorsFile, PollMetricsFile, PendingDeletesFile,
	// WorkstreamsFile, WorkstreamProposalsFile — no meaningful "latest
	// activity" timestamp for the list --since use case.
	return time.Time{}, nil
}

// scanLatestTs walks a JSONL file line by line and returns the latest
// timestamp found in any of the named top-level fields. Lines that fail to
// parse as JSON, or whose matched field fails to parse as a timestamp,
// surface as errors with line-number context — keeping pigeon's stores
// fail-loud on real corruption rather than degrading to a stale timestamp.
// Lines that simply lack the named fields are tolerated; not every line in
// a file carries every field (separators, mixed line types in threads).
func scanLatestTs(path string, fields ...string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Email HTML bodies can be large; bump the per-line buffer past the
	// 64KB default.
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
				return time.Time{}, fmt.Errorf("parse %q on line %d in %s: %w", field, lineNum, path, err)
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

// latestDriveMetaDate returns the newest drive-meta-YYYY-MM-DD.json date
// found in dir. Used by Drive content kinds (Tab/Sheet/Formula) — their
// authoritative "when was this file last modified" lives in the sibling
// meta filename, not in their own bytes.
func latestDriveMetaDate(dir string) (time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, fmt.Errorf("read drive dir %s: %w", dir, err)
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		meta, ok, err := paths.ParseDriveMetaPath(filepath.Join(dir, entry.Name()))
		if err != nil {
			return time.Time{}, fmt.Errorf("parse drive-meta %s: %w", entry.Name(), err)
		}
		if !ok {
			continue
		}
		if d := meta.Date(); d.After(latest) {
			latest = d
		}
	}
	return latest, nil
}
