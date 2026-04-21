package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// activeConv represents a recent source discovered from file paths.
type activeConv struct {
	Display    string    // user-facing source identifier
	Dir        string    // absolute source directory or file path
	LatestTime time.Time // most recent activity timestamp from file content or metadata
}

type recentFile struct {
	path  string
	root  string
	parts []string
}

type recentSourceHandler interface {
	Match(file recentFile) (activeConv, bool, error)
}

var recentSourceHandlers = []recentSourceHandler{
	messagingRecentSourceHandler{},
	gwsRecentSourceHandler{},
	linearRecentSourceHandler{},
}

// extractConversations deduplicates matched files into unique sources,
// tracking the most recent activity timestamp per source.
func extractConversations(files []string, root string) ([]activeConv, error) {
	seen := make(map[string]*activeConv)
	var order []string
	for _, path := range files {
		src, ok, err := describeRecentSource(path, root)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if !ok {
			continue
		}

		c, found := seen[src.Dir]
		if !found {
			c = &activeConv{
				Display: src.Display,
				Dir:     src.Dir,
			}
			seen[src.Dir] = c
			order = append(order, src.Dir)
		}
		if src.LatestTime.After(c.LatestTime) {
			c.LatestTime = src.LatestTime
		}
	}

	result := make([]activeConv, len(order))
	for i, key := range order {
		result[i] = *seen[key]
	}
	return result, nil
}

func describeRecentSource(path, root string) (activeConv, bool, error) {
	file, ok := newRecentFile(path, root)
	if !ok {
		return activeConv{}, false, nil
	}
	for _, handler := range recentSourceHandlers {
		src, matched, err := handler.Match(file)
		if err != nil {
			return activeConv{}, false, err
		}
		if matched {
			return src, true, nil
		}
	}
	return activeConv{}, false, nil
}

func newRecentFile(path, root string) (recentFile, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return recentFile{}, false
	}
	parentPrefix := ".." + string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, parentPrefix) {
		return recentFile{}, false
	}

	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 4 {
		return recentFile{}, false
	}
	return recentFile{
		path:  path,
		root:  root,
		parts: parts,
	}, true
}

type messagingRecentSourceHandler struct{}

func (messagingRecentSourceHandler) Match(file recentFile) (activeConv, bool, error) {
	if file.parts[0] != "slack" && file.parts[0] != "whatsapp" {
		return activeConv{}, false, nil
	}

	parts := append([]string(nil), file.parts...)
	isThread := paths.IsThreadFile(file.path)
	if isThread {
		for i, p := range parts {
			if p == paths.ThreadsSubdir {
				parts = append(parts[:i], parts[i+1:]...)
				break
			}
		}
	}
	if len(parts) < 4 {
		return activeConv{}, false, nil
	}

	ts, err := latestMessagingTime(file.path, isThread)
	if err != nil {
		return activeConv{}, false, err
	}
	return activeConv{
		Display:    strings.Join(parts[:3], "/"),
		Dir:        filepath.Join(file.root, parts[0], parts[1], parts[2]),
		LatestTime: ts,
	}, true, nil
}

type gwsRecentSourceHandler struct{}

func (gwsRecentSourceHandler) Match(file recentFile) (activeConv, bool, error) {
	if file.parts[0] != "gws" {
		return activeConv{}, false, nil
	}
	service := file.parts[2]
	switch service {
	case paths.GmailSubdir:
		ts, err := latestJSONLTime(file.path)
		if err != nil {
			return activeConv{}, false, err
		}
		return activeConv{
			Display:    strings.Join(file.parts[:3], "/"),
			Dir:        filepath.Join(file.root, file.parts[0], file.parts[1], file.parts[2]),
			LatestTime: ts,
		}, true, nil

	case paths.GcalendarSubdir:
		if len(file.parts) < 5 {
			return activeConv{}, false, nil
		}
		ts, err := latestJSONLTime(file.path)
		if err != nil {
			return activeConv{}, false, err
		}
		return activeConv{
			Display:    strings.Join(file.parts[:4], "/"),
			Dir:        filepath.Join(file.root, file.parts[0], file.parts[1], file.parts[2], file.parts[3]),
			LatestTime: ts,
		}, true, nil

	case paths.GdriveSubdir:
		if len(file.parts) < 5 {
			return activeConv{}, false, nil
		}
		dir := filepath.Join(file.root, file.parts[0], file.parts[1], file.parts[2], file.parts[3])
		ts, err := latestDriveTime(dir)
		if err != nil {
			return activeConv{}, false, err
		}
		return activeConv{
			Display:    strings.Join(file.parts[:4], "/"),
			Dir:        dir,
			LatestTime: ts,
		}, true, nil
	}

	return activeConv{}, false, nil
}

type linearRecentSourceHandler struct{}

func (linearRecentSourceHandler) Match(file recentFile) (activeConv, bool, error) {
	if file.parts[0] != "linear-issues" || len(file.parts) < 4 || file.parts[2] != "issues" {
		return activeConv{}, false, nil
	}

	ts, err := latestJSONLTime(file.path)
	if err != nil {
		return activeConv{}, false, err
	}
	return activeConv{
		Display:    strings.Join([]string{file.parts[0], file.parts[1], strings.TrimSuffix(file.parts[3], paths.FileExt)}, "/"),
		Dir:        filepath.Join(file.root, file.parts[0], file.parts[1], file.parts[2], file.parts[3]),
		LatestTime: ts,
	}, true, nil
}

func latestMessagingTime(path string, isThread bool) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	if isThread {
		tf, err := modelv1.ParseThreadFile(data)
		if err != nil {
			return time.Time{}, err
		}
		latest := tf.Parent.Ts
		for _, r := range tf.Replies {
			if r.Ts.After(latest) {
				latest = r.Ts
			}
		}
		return latest, nil
	}

	df, err := modelv1.ParseDateFile(data)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, m := range df.Messages {
		if m.Ts.After(latest) {
			latest = m.Ts
		}
	}
	return latest, nil
}

func latestJSONLTime(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}

	var latest time.Time
	for _, raw := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if raw == "" {
			continue
		}
		line, err := modelv1.Parse(raw)
		if err != nil {
			return time.Time{}, err
		}
		ts := lineActivityTime(line)
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest, nil
}

func lineActivityTime(line modelv1.Line) time.Time {
	if ts := line.Ts(); !ts.IsZero() {
		return ts
	}
	if line.Type == modelv1.LineEvent && line.Event != nil {
		return calendarEventTime(line.Event)
	}
	return time.Time{}
}

func calendarEventTime(event *modelv1.CalendarEvent) time.Time {
	if event == nil {
		return time.Time{}
	}
	if event.Runtime.Start != nil {
		if t, err := time.Parse(time.RFC3339, event.Runtime.Start.DateTime); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", event.Runtime.Start.Date); err == nil {
			return t
		}
	}
	if event.Runtime.OriginalStartTime != nil {
		if t, err := time.Parse(time.RFC3339, event.Runtime.OriginalStartTime.DateTime); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02", event.Runtime.OriginalStartTime.Date); err == nil {
			return t
		}
	}
	if t, err := time.Parse(time.RFC3339, event.Runtime.Updated); err == nil {
		return t
	}
	return time.Time{}
}

func latestDriveTime(dir string) (time.Time, error) {
	metaPaths, err := filepath.Glob(filepath.Join(dir, paths.DriveMetaFileGlob))
	if err != nil {
		return time.Time{}, err
	}

	var latest time.Time
	for _, metaPath := range metaPaths {
		ts, err := driveMetaTime(metaPath)
		if err != nil {
			return time.Time{}, err
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest, nil
}

func driveMetaTime(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}

	var meta modelv1.DocMeta
	if err := json.Unmarshal(data, &meta); err == nil && meta.ModifiedTime != "" {
		if t, err := time.Parse(time.RFC3339, meta.ModifiedTime); err == nil {
			return t, nil
		}
	}

	base := filepath.Base(path)
	dateStr := strings.TrimSuffix(strings.TrimPrefix(base, "drive-meta-"), ".json")
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}
