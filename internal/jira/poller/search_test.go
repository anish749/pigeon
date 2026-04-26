package poller

import "testing"

func TestJQLEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ENG", `"ENG"`},
		{"My Project", `"My Project"`},
		{`with "quote"`, `"with \"quote\""`},
		{`back\slash`, `"back\\slash"`},
		{"", `""`},
	}
	for _, c := range cases {
		if got := jqlEscape(c.in); got != c.want {
			t.Errorf("jqlEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
