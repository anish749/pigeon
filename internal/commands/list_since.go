package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/timeutil"
	"github.com/anish749/pigeon/internal/workspace"
)

// RunListSince prints listConvs with activity within the given duration
// window, scoped by the active workspace and optional platform/account.
// Each line is "<display>  last: <age> ago" followed by the listConv
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
		fmt.Println("No listConvs found.")
		return nil
	}

	root := paths.DefaultDataRoot().Path()
	convs, err := extractConversations(allFiles, root)
	if err != nil {
		return err
	}
	if len(convs) == 0 {
		fmt.Println("No listConvs found.")
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

// activeConv represents a listConv discovered from file paths.
type activeConv struct {
	Display    string    // platform/account/listConv
	Dir        string    // absolute listConv directory
	LatestTime time.Time // most recent activity timestamp
}

// extractConversations deduplicates files into unique listConvs,
// tracking the most recent activity timestamp per listConv. Grouping
// granularity is per-kind so each source surfaces at its natural unit:
// messaging listConvs group all date+thread files under one dir, but
// each Drive doc, each calendar, and each Linear issue stand alone.
func extractConversations(files []paths.DataFile, root string) ([]activeConv, error) {
	seen := make(map[string]*activeConv)
	var order []string
	for _, f := range files {
		conv, ok := listConvFor(f, root)
		if !ok {
			continue
		}

		c, exists := seen[conv.Dir]
		if !exists {
			c = &activeConv{Display: conv.Display, Dir: conv.Dir}
			seen[conv.Dir] = c
			order = append(order, conv.Dir)
		}

		ts, err := LatestTs(f)
		if err != nil {
			return nil, fmt.Errorf("latest ts %s: %w", f.Path(), err)
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

// listConv is the grouping identity used by `list --since`. Dir is the
// uniqueness key (the directory or file that defines a listConv in this
// view); Display is the user-facing label.
type listConv struct {
	Dir     string
	Display string
}

// listConvFor returns the listConv that owns f, dispatched on the
// typed paths.DataFile so each kind groups at its own natural unit. The
// boolean is false when f belongs to a kind that does not surface in
// `list --since` output (sidecars, queues, identity files, attachments).
func listConvFor(f paths.DataFile, root string) (listConv, bool) {
	switch v := f.(type) {
	case paths.MessagingDateFile:
		// <plat>/<acct>/<conv>/YYYY-MM-DD.jsonl — parent dir is the listConv.
		return relativeConv(filepath.Dir(v.Path()), root), true
	case paths.ThreadFile:
		// <plat>/<acct>/<conv>/threads/<ts>.jsonl — strip /threads/<ts>.jsonl.
		return relativeConv(filepath.Dir(filepath.Dir(v.Path())), root), true
	case paths.EmailDateFile:
		// gws/<acct>/gmail/YYYY-MM-DD.jsonl — group at the gmail dir
		// (one stream per account, no per-day split in the listing).
		return relativeConv(filepath.Dir(v.Path()), root), true
	case paths.CalendarDateFile:
		// gws/<acct>/gcalendar/<calID>/YYYY-MM-DD.jsonl — per-calendar.
		return relativeConv(filepath.Dir(v.Path()), root), true
	case paths.TabFile, paths.SheetFile, paths.FormulaFile, paths.CommentsFile, paths.DriveMetaFile:
		// gws/<acct>/gdrive/<doc>/{Notes.md,Sheet.csv,comments.jsonl,drive-meta-*.json}
		// — per-doc dir.
		return relativeConv(filepath.Dir(f.Path()), root), true
	case paths.IssueFile:
		// linear-issues/<acct>/issues/<id>.jsonl — each issue is its own
		// listConv. Dir is the file itself (no per-issue subdir);
		// Display drops the redundant "issues" segment for readability.
		display, err := filepath.Rel(root, v.Path())
		if err != nil {
			display = v.Path()
		}
		display = strings.Replace(display, string(filepath.Separator)+"issues"+string(filepath.Separator), string(filepath.Separator), 1)
		return listConv{Dir: v.Path(), Display: display}, true
	}
	// AttachmentFile, ConvMetaFile, PeopleFile, MaintenanceFile,
	// SyncCursorsFile, PollMetricsFile, PendingDeletesFile, WorkstreamsFile,
	// WorkstreamProposalsFile — not surfaced in list --since.
	return listConv{}, false
}

// relativeConv builds a listConv whose Dir is convDir and whose Display
// is convDir relative to the data root. Falls back to the absolute path if
// Rel fails (different volumes).
func relativeConv(convDir, root string) listConv {
	display, err := filepath.Rel(root, convDir)
	if err != nil {
		display = convDir
	}
	return listConv{Dir: convDir, Display: display}
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
		return v.Date()
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
//
// Uses bufio.Reader.ReadBytes('\n') rather than bufio.Scanner so there is
// no per-line size cap to tune — pigeon stores can hold large lines (email
// HTML bodies, base64 attachments) and the read allocation grows to fit.
// Same idiom the monitor SSE consumer uses (commands/monitor.go).
func scanLatestTs(path string, fields ...string) (time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var latest time.Time
	lineNum := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(trimmed, &raw); err != nil {
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
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return latest, nil
			}
			return time.Time{}, fmt.Errorf("read %s: %w", path, readErr)
		}
	}
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
		d, err := meta.Date()
		if err != nil {
			return time.Time{}, fmt.Errorf("drive-meta %s: %w", entry.Name(), err)
		}
		if d.After(latest) {
			latest = d
		}
	}
	return latest, nil
}
