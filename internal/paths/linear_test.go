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
			path: "/data/linear-issues/acme/issues/PROJ-123.jsonl",
			want: true,
		},
		{
			name: "workspace named with dashes",
			path: "/data/linear-issues/my-team/issues/ENG-42.jsonl",
			want: true,
		},
		{
			name: "wrong extension",
			path: "/data/linear-issues/acme/issues/PROJ-123.md",
			want: false,
		},
		{
			name: "wrong parent directory",
			path: "/data/linear-issues/acme/threads/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "path not under linear-issues platform",
			path: "/data/slack/acme/issues/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "bare 'linear' platform segment is not linear-issues",
			// Guards against the original bug where LinearPlatform was "linear"
			// but the real platform segment is "linear-issues". This path must
			// NOT match.
			path: "/data/linear/acme/issues/PROJ-123.jsonl",
			want: false,
		},
		{
			name: "channel name containing linear-issues is not a platform segment",
			// A slack workspace slug that happens to contain "linear-issues"
			// as a substring must not false-match. The helper requires it as
			// a separator-delimited segment.
			path: "/data/slack/acme/#linear-issues-updates/issues/PROJ-123.jsonl",
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
