package api

import "testing"

func TestLooksLikeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"alice@example.com", true},
		{"user@example.co.uk", true},
		{"a@b.c", true},
		{"@Someone", false},
		{"#engineering", false},
		{"U02KNCK11JM", false},
		{"@mpdm-alice--bob-1", false},
		{"Alice Smith", false},
		{"", false},
		{"@", false},
		{"@foo", false},
		{"foo@", false},
		{"foo@bar", false},
	}

	for _, tt := range tests {
		if got := looksLikeEmail(tt.input); got != tt.want {
			t.Errorf("looksLikeEmail(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
