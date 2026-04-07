package timeutil

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1h30m", time.Hour + 30*time.Minute},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.input)
		if err != nil {
			t.Errorf("ParseDuration(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseDurationErrors(t *testing.T) {
	for _, input := range []string{"", "abc", "d", "xd"} {
		_, err := ParseDuration(input)
		if err == nil {
			t.Errorf("ParseDuration(%q) expected error, got nil", input)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{5 * time.Minute, "5m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{2 * time.Hour, "2h"},
		{time.Hour + 30*time.Minute, "1h30m"},
		{23*time.Hour + 59*time.Minute, "23h59m"},
		{24 * time.Hour, "1d"},
		{7 * 24 * time.Hour, "7d"},
		{30 * 24 * time.Hour, "30d"},
	}
	for _, tt := range tests {
		got := FormatAge(tt.input)
		if got != tt.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestRoundTrip verifies that FormatAge output can be parsed back by
// ParseDuration to a value equal to the original (for durations where
// FormatAge doesn't truncate).
func TestRoundTrip(t *testing.T) {
	tests := []time.Duration{
		0,
		30 * time.Second,
		time.Minute,
		5 * time.Minute,
		59 * time.Minute,
		time.Hour,
		2 * time.Hour,
		time.Hour + 30*time.Minute,
		5*time.Hour + 45*time.Minute,
		24 * time.Hour,
		7 * 24 * time.Hour,
		30 * 24 * time.Hour,
	}
	for _, d := range tests {
		formatted := FormatAge(d)
		parsed, err := ParseDuration(formatted)
		if err != nil {
			t.Errorf("round-trip failed: FormatAge(%v) = %q, ParseDuration error: %v", d, formatted, err)
			continue
		}
		if parsed != d {
			t.Errorf("round-trip mismatch: FormatAge(%v) = %q, ParseDuration(%q) = %v", d, formatted, formatted, parsed)
		}
	}
}
