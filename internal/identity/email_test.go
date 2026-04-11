package identity

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Basic case folding.
		{"Alice@Company.com", "alice@company.com"},
		{"ALICE@COMPANY.COM", "alice@company.com"},

		// Gmail dot insensitivity.
		{"a.li.ce@gmail.com", "alice@gmail.com"},
		{"alice@gmail.com", "alice@gmail.com"},

		// Gmail plus addressing.
		{"alice+newsletter@gmail.com", "alice@gmail.com"},
		{"alice+tag@googlemail.com", "alice@googlemail.com"},

		// Gmail dots + plus combined.
		{"a.li.ce+tag@gmail.com", "alice@gmail.com"},

		// Non-Gmail: dots and plus are preserved (only lowered).
		{"a.li.ce@company.com", "a.li.ce@company.com"},
		{"alice+tag@company.com", "alice+tag@company.com"},

		// No @ sign — just lowercase.
		{"ALICE", "alice"},
	}

	for _, tt := range tests {
		got := NormalizeEmail(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchesEmail_CaseInsensitive(t *testing.T) {
	p := Person{Email: []string{"alice@company.com"}}
	if !p.matchesEmail("Alice@Company.com") {
		t.Error("should match case-insensitively")
	}
}

func TestMatchesEmail_GmailDots(t *testing.T) {
	p := Person{Email: []string{"a.lice@gmail.com"}}
	if !p.matchesEmail("alice@gmail.com") {
		t.Error("should match Gmail with dots removed")
	}
}

func TestMatchesEmail_GmailPlus(t *testing.T) {
	p := Person{Email: []string{"alice@gmail.com"}}
	if !p.matchesEmail("alice+work@gmail.com") {
		t.Error("should match Gmail with plus suffix stripped")
	}
}

func TestMatchesEmail_NonGmailDotsNotIgnored(t *testing.T) {
	p := Person{Email: []string{"a.lice@company.com"}}
	if p.matchesEmail("alice@company.com") {
		t.Error("should NOT ignore dots for non-Gmail domains")
	}
}
