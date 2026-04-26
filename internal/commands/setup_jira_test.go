package commands

import (
	"fmt"
	"strings"
	"testing"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/google/go-cmp/cmp"

	"github.com/anish749/pigeon/internal/config"
)

// TestExplainMeError verifies the per-status-code remediation for a
// failed Me() call. The 401 path is the most-hit failure mode for
// first-time users and must surface the "SSO password is not the API
// token" guidance with the token-generation URL inline; other codes
// pass through with enough detail to debug.
func TestExplainMeError(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantContains []string
	}{
		{
			name:         "401 names SSO and links the token URL",
			err:          fmt.Errorf("up: %w", &jira.ErrUnexpectedResponse{StatusCode: 401, Status: "401 Unauthorized"}),
			wantContains: []string{"401 Unauthorized", "SSO", atlassianAPITokenURL, "pigeon setup-jira"},
		},
		{
			name:         "403 names the permission cause",
			err:          fmt.Errorf("up: %w", &jira.ErrUnexpectedResponse{StatusCode: 403, Status: "403 Forbidden"}),
			wantContains: []string{"403 Forbidden", "permission"},
		},
		{
			name:         "404 names a server URL typo",
			err:          fmt.Errorf("up: %w", &jira.ErrUnexpectedResponse{StatusCode: 404, Status: "404 Not Found"}),
			wantContains: []string{"404 Not Found", "server"},
		},
		{
			name:         "other status codes pass through with status + body",
			err:          fmt.Errorf("up: %w", &jira.ErrUnexpectedResponse{StatusCode: 500, Status: "500 Server Error"}),
			wantContains: []string{"500 Server Error"},
		},
		{
			name:         "non-HTTP error passes through",
			err:          fmt.Errorf("dial tcp: connection refused"),
			wantContains: []string{"connection refused"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := explainMeError("https://acme.atlassian.net", "alice@acme.com", c.err)
			msg := got.Error()
			for _, s := range c.wantContains {
				if !strings.Contains(msg, s) {
					t.Errorf("error message missing %q\nfull error:\n%s", s, msg)
				}
			}
		})
	}
}

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
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-1", Account: "acme"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-1", Account: "acme"}},
		},
		{
			name: "same resolved path - replaces existing",
			existing: []config.JiraConfig{
				{JiraConfig: defaultResolved, APIToken: "tok-old", Account: "acme"},
			},
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-new", Account: "acme"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-new", Account: "acme"}},
		},
		{
			name: "empty raw existing collapses with explicit new",
			existing: []config.JiraConfig{
				{JiraConfig: "", APIToken: "tok-old", Account: "acme"},
			},
			newEntry: config.JiraConfig{JiraConfig: defaultResolved, APIToken: "tok-new", Account: "acme"},
			resolved: defaultResolved,
			want:     []config.JiraConfig{{JiraConfig: defaultResolved, APIToken: "tok-new", Account: "acme"}},
		},
		{
			name: "different resolved path appends",
			existing: []config.JiraConfig{
				{JiraConfig: "/site-a.yml", APIToken: "tok-a", Account: "site-a"},
			},
			newEntry: config.JiraConfig{JiraConfig: "/site-b.yml", APIToken: "tok-b", Account: "site-b"},
			resolved: "/site-b.yml",
			want: []config.JiraConfig{
				{JiraConfig: "/site-a.yml", APIToken: "tok-a", Account: "site-a"},
				{JiraConfig: "/site-b.yml", APIToken: "tok-b", Account: "site-b"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{Jira: append([]config.JiraConfig(nil), c.existing...)}
			upsertJiraByResolvedPath(cfg, c.newEntry, c.resolved)
			if diff := cmp.Diff(c.want, cfg.Jira); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
