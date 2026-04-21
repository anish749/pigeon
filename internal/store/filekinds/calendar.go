package filekinds

import (
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// calendarDateKind matches calendar date files:
//
//	<root>/gws/<account>/gcalendar/<calID>/YYYY-MM-DD.jsonl
//
// Calendar events do not carry a top-level "ts" field. The authoritative
// last-modified signal is the "updated" field (RFC3339, always set by the
// Google Calendar API); "created" is also present and is used as a floor so
// that events with a newer created-than-updated anomaly still contribute.
type calendarDateKind struct{}

func (calendarDateKind) Name() string { return "calendar-date" }

func (calendarDateKind) Match(path string) bool {
	if !paths.IsDateFile(filepath.Base(path)) {
		return false
	}
	return pathHasSegment(path, paths.GcalendarSubdir)
}

func (calendarDateKind) LatestTs(path string) (time.Time, error) {
	return scanLatestTs(path, "updated", "created")
}

func (calendarDateKind) Conversation(path, root string) Conversation {
	// One conversation per calendar (primary, team, etc.) — the parent dir
	// of the YYYY-MM-DD.jsonl file is <account>/gcalendar/<calID>.
	return relativeConv(filepath.Dir(path), root)
}
