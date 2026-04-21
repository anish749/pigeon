package paths

import "testing"

func TestIsLinearIssueFile(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "canonical linear issue path",
			path: "/data/linear/acme/issues/PROJ-123.jsonl",
			want: true,
		},
		{
			name: "workspace named with dashes",
			path: "/data/linear/my-team/issues/ENG-42.jsonl",
			want: true,
		},
		{
			name: "wrong extension",
			path: "/data/linear/acme/issues/PROJ-123.md",
			want: false,
		},
		{
			name: "wrong parent directory",
			path: "/data/linear/acme/threads/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "path not under linear platform",
			path: "/data/slack/acme/issues/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "linear deeper than platform segment",
			// A slack workspace slug that happens to contain "linear" as a
			// component must not false-match. The helper requires a /linear/
			// separator-delimited segment somewhere in the path — this has
			// "#linear-updates" which is NOT a /linear/ segment.
			path: "/data/slack/acme/#linear-updates/issues/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "no directory",
			path: "PROJ-123.jsonl",
			want: false,
		},
		{
			name: "empty",
			path: "",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLinearIssueFile(tt.path); got != tt.want {
				t.Errorf("IsLinearIssueFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
