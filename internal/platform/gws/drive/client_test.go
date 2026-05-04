package drive

import (
	"encoding/json"
	"testing"
)

func TestCellToString(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"quoted string", `"hello"`, "hello"},
		{"formula", `"=SUM(A1:A10)"`, "=SUM(A1:A10)"},
		{"integer", `42`, "42"},
		{"float", `3.14`, "3.14"},
		{"boolean true", `true`, "true"},
		{"boolean false", `false`, "false"},
		{"null", `null`, "null"},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cellToString(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("cellToString(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
