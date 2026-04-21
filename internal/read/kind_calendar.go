package read

import (
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// calendarDateKind matches calendar date files:
//
//	<root>/gws/<account>/gcalendar/<calID>/YYYY-MM-DD.jsonl
//
// Calendar event lines do not carry a top-level "ts" field — their temporal
// signals are "updated", "created", and "start.dateTime". Surfacing a latest
// timestamp from those is a separate change; for now the kind is registered
// so calendar paths are recognised (rather than falling through to the
// generic messaging matcher) and contribute nothing to list --since age
// display — the pre-registry behaviour for calendar date files.
type calendarDateKind struct{}

func (calendarDateKind) Name() string { return "calendar-date" }

func (calendarDateKind) Match(path string) bool {
	if !paths.IsDateFile(filepath.Base(path)) {
		return false
	}
	return pathHasSegment(path, paths.GcalendarSubdir)
}

func (calendarDateKind) LatestTs(_ string) (time.Time, error) {
	return time.Time{}, nil
}
