package poller

import "testing"

func TestJqlCutoff(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"empty passes through", "", "", false},
		{"jira numeric-offset", "2026-04-05T09:44:15.076+0000", "2026-04-05 09:44", false},
		{"jira numeric-offset non-utc", "2026-04-05T09:44:15.076-0700", "2026-04-05 16:44", false},
		{"rfc3339 Z", "2026-04-05T09:44:15Z", "2026-04-05 09:44", false},
		{"rfc3339 with offset", "2026-04-05T09:44:15+02:00", "2026-04-05 07:44", false},
		{"garbage rejected", "banana", "", true},
		{"truncated rejected", "2026-04", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := jqlCutoff(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("jqlCutoff(%q) = %q, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("jqlCutoff(%q) error: %v", c.in, err)
				return
			}
			if got != c.want {
				t.Errorf("jqlCutoff(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
