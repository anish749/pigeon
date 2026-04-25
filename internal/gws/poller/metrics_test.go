package poller

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

func TestAppendMetric_CreatesFileAndAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", ".poll-metrics.jsonl")

	m1 := PollMetric{
		Ts:         time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
		Service:    "gmail",
		DurationMs: 142,
		Changes:    3,
	}
	if err := appendMetric(paths.PollMetricsFile(path), m1); err != nil {
		t.Fatalf("first appendMetric: %v", err)
	}

	m2 := PollMetric{
		Ts:         time.Date(2026, 4, 9, 12, 0, 20, 0, time.UTC),
		Service:    "calendar",
		DurationMs: 58,
		Changes:    0,
		Err:        "rate limited",
	}
	if err := appendMetric(paths.PollMetricsFile(path), m2); err != nil {
		t.Fatalf("second appendMetric: %v", err)
	}

	// Both records must be present, one per line, parseable as JSON.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var got []PollMetric
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var m PollMetric
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal line %q: %v", line, err)
		}
		got = append(got, m)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
	if got[0].Service != "gmail" || got[0].Changes != 3 || got[0].DurationMs != 142 {
		t.Errorf("record 0 wrong: %+v", got[0])
	}
	if got[1].Service != "calendar" || got[1].Changes != 0 || got[1].Err != "rate limited" {
		t.Errorf("record 1 wrong: %+v", got[1])
	}
	if !got[0].Ts.Equal(m1.Ts) {
		t.Errorf("record 0 ts mismatch: want %v got %v", m1.Ts, got[0].Ts)
	}
}

func TestAppendMetric_OmitsEmptyErr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".poll-metrics.jsonl")

	m := PollMetric{
		Ts:         time.Now().UTC(),
		Service:    "drive",
		DurationMs: 10,
		Changes:    0,
	}
	if err := appendMetric(paths.PollMetricsFile(path), m); err != nil {
		t.Fatalf("appendMetric: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// A successful poll should not serialize the err field at all.
	if strings.Contains(string(data), `"err"`) {
		t.Errorf("unexpected err field in %q", string(data))
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("line must end with newline: %q", string(data))
	}
}
