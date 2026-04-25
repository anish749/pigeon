package poller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// PollMetric is a single observation of one poll of one GWS service. One
// record is appended to <account>/.poll-metrics.jsonl per service per poll
// cycle (so three records per cycle: gmail, calendar, drive).
//
// The file is intended as raw telemetry for offline analysis: hit rate
// (what fraction of polls had Changes > 0), latency distribution, and
// burstiness of changes over time. These answer the question of whether
// the fixed interval should be replaced with a debouncer or adaptive
// poll rate.
type PollMetric struct {
	// Ts is when the poll started, in UTC.
	Ts time.Time `json:"ts"`
	// Service is "gmail", "calendar", or "drive".
	Service string `json:"service"`
	// DurationMs is the wall-clock duration of the poll call in milliseconds.
	DurationMs int64 `json:"duration_ms"`
	// Changes is the number of items observed by this poll. For gmail it
	// is added+deleted; for calendar, events+recurring; for drive, changes.
	// During initial seed/backfill it reflects the backfilled item count.
	Changes int `json:"changes"`
	// Err is the error string if the poll failed, empty otherwise.
	Err string `json:"err,omitempty"`
}

// appendMetric appends a PollMetric as a JSONL line to the given file.
// Creates the file and parent directories if they don't exist.
func appendMetric(path paths.PollMetricsFile, m PollMetric) error {
	p := path.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", p, err)
	}

	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal metric: %w", err)
	}

	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write to %s: %w", p, err)
	}
	return nil
}
