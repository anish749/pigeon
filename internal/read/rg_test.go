package read

import (
	"testing"
	"time"
)

func TestDateGlobs(t *testing.T) {
	tests := []struct {
		name  string
		since time.Duration
		want  int // minimum expected globs
	}{
		{"1 day", 24 * time.Hour, 1},
		{"2 days", 48 * time.Hour, 2},
		{"7 days", 7 * 24 * time.Hour, 7},
		{"1 hour", 1 * time.Hour, 1}, // still covers today
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globs := dateGlobs(tt.since)
			if len(globs) < tt.want {
				t.Errorf("dateGlobs(%v) returned %d globs, want at least %d", tt.since, len(globs), tt.want)
			}
			// Each glob should end with .jsonl and start with a date.
			for _, g := range globs {
				if len(g) != len("YYYY-MM-DD.jsonl") {
					t.Errorf("unexpected glob format: %q", g)
				}
			}
		})
	}
}

func TestDateGlobs_ContainsToday(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02") + ".jsonl"
	globs := dateGlobs(1 * time.Hour)

	found := false
	for _, g := range globs {
		if g == today {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dateGlobs(1h) should contain today %q, got %v", today, globs)
	}
}

func TestDateGlobs_ContainsCutoffDate(t *testing.T) {
	cutoff := time.Now().UTC().Add(-3 * 24 * time.Hour).Truncate(24 * time.Hour)
	cutoffStr := cutoff.Format("2006-01-02") + ".jsonl"
	globs := dateGlobs(3 * 24 * time.Hour)

	found := false
	for _, g := range globs {
		if g == cutoffStr {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("dateGlobs(3d) should contain cutoff date %q, got %v", cutoffStr, globs)
	}
}

func TestThreadDatePatterns(t *testing.T) {
	patterns := threadDatePatterns(3 * 24 * time.Hour)

	if len(patterns) < 3 {
		t.Fatalf("threadDatePatterns(3d) returned %d patterns, want at least 3", len(patterns))
	}

	today := `"ts":"` + time.Now().UTC().Format("2006-01-02")
	found := false
	for _, p := range patterns {
		if p == today {
			found = true
		}
		// Each pattern should be a ts prefix.
		if len(p) != len(`"ts":"YYYY-MM-DD`) {
			t.Errorf("unexpected pattern format: %q", p)
		}
	}
	if !found {
		t.Errorf("threadDatePatterns should contain today %q, got %v", today, patterns)
	}
}

func TestThreadDatePatterns_MatchesDateGlobs(t *testing.T) {
	since := 5 * 24 * time.Hour
	globs := dateGlobs(since)
	patterns := threadDatePatterns(since)

	if len(globs) != len(patterns) {
		t.Errorf("dateGlobs returned %d entries, threadDatePatterns returned %d — should match", len(globs), len(patterns))
	}
}

func TestReverseStrings(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{nil, nil},
		{[]string{"a"}, []string{"a"}},
		{[]string{"a", "b", "c"}, []string{"c", "b", "a"}},
		{[]string{"1", "2", "3", "4"}, []string{"4", "3", "2", "1"}},
	}
	for _, tt := range tests {
		got := make([]string, len(tt.input))
		copy(got, tt.input)
		reverseStrings(got)
		if len(got) != len(tt.want) {
			t.Errorf("reverseStrings(%v) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("reverseStrings(%v) = %v, want %v", tt.input, got, tt.want)
				break
			}
		}
	}
}

func TestRunRg_MissingDir(t *testing.T) {
	_, err := runRg([]string{"--files", "/nonexistent/path"}, "/nonexistent/path")
	if err == nil {
		t.Error("runRg on missing dir should return error")
	}
}
