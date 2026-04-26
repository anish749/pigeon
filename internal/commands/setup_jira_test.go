package commands

import (
	"testing"

	"github.com/anish749/pigeon/internal/config"
)

// TestUpsertJiraByResolvedPath verifies the setup-jira idempotency: a
// second setup that resolves to the same file must replace the first
// entry, regardless of raw spelling. AddJira's literal-string dedupe
// is correct for hand-edit semantics; setup-jira's idempotency is
// resolved-path-based, hence this helper.
func TestUpsertJiraByResolvedPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	t.Setenv("JIRA_CONFIG_FILE", "")
	defaultResolved := "/tmp/xdgtest/.jira/.config.yml"

	cases := []struct {
		name     string
		existing []config.JiraConfig
		newEntry config.JiraConfig
		resolved string
		want     []config.JiraConfig
	}{
		{
			name:     "empty config appends",
			existing: nil,
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-1"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-1"}},
		},
		{
			name: "same resolved path - replaces existing",
			existing: []config.JiraConfig{
				{JiraConfig: defaultResolved, APIToken: "tok-old"},
			},
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-new"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-new"}},
		},
		{
			name: "empty raw existing collapses with explicit new",
			existing: []config.JiraConfig{
				{JiraConfig: "", APIToken: "tok-old"},
			},
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-new"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-new"}},
		},
		{
			name: "different resolved path appends",
			existing: []config.JiraConfig{
				{JiraConfig: "/site-a.yml", APIToken: "tok-a"},
			},
			newEntry: config.JiraConfig{JiraConfig: "/site-b.yml", APIToken: "tok-b"},
			resolved: "/site-b.yml",
			want: []config.JiraConfig{
				{JiraConfig: "/site-a.yml", APIToken: "tok-a"},
				{JiraConfig: "/site-b.yml", APIToken: "tok-b"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{Jira: append([]config.JiraConfig(nil), c.existing...)}
			upsertJiraByResolvedPath(cfg, c.newEntry, c.resolved)
			if !equalJira(cfg.Jira, c.want) {
				t.Errorf("got %+v, want %+v", cfg.Jira, c.want)
			}
		})
	}
}

func equalJira(a, b []config.JiraConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
