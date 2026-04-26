package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/config"
)

// TestFindResolvedConflict exercises the conflict detection that runs
// before AddJira during setup. The cases cover the realistic shapes:
// no conflict, same-raw upsert (not a conflict — AddJira handles it),
// and the actual collision (different raw, same resolved path).
func TestFindResolvedConflict(t *testing.T) {
	// All cases use a controlled jira-cli config home so the resolution
	// of jira_config: "" is predictable.
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")
	t.Setenv("JIRA_CONFIG_FILE", "")

	defaultResolved := "/tmp/xdgtest/.jira/.config.yml"

	cases := []struct {
		name         string
		existing     []config.JiraConfig
		newRaw       string
		newResolved  string
		wantConflict bool
	}{
		{
			name:         "empty config, no conflict",
			existing:     nil,
			newRaw:       "",
			newResolved:  defaultResolved,
			wantConflict: false,
		},
		{
			name:         "same raw - upsert path, not a conflict",
			existing:     []config.JiraConfig{{JiraConfig: ""}},
			newRaw:       "",
			newResolved:  defaultResolved,
			wantConflict: false,
		},
		{
			name:         "same raw explicit - upsert path, not a conflict",
			existing:     []config.JiraConfig{{JiraConfig: "/some/path.yml"}},
			newRaw:       "/some/path.yml",
			newResolved:  "/some/path.yml",
			wantConflict: false,
		},
		{
			name:         "different raw, different resolved - distinct entries",
			existing:     []config.JiraConfig{{JiraConfig: "/site-a.yml"}},
			newRaw:       "/site-b.yml",
			newResolved:  "/site-b.yml",
			wantConflict: false,
		},
		{
			name: "empty existing collides with explicit default path",
			existing: []config.JiraConfig{
				{JiraConfig: ""},
			},
			newRaw:       defaultResolved,
			newResolved:  defaultResolved,
			wantConflict: true,
		},
		{
			name: "explicit existing collides with empty new",
			existing: []config.JiraConfig{
				{JiraConfig: defaultResolved},
			},
			newRaw:       "",
			newResolved:  defaultResolved,
			wantConflict: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &config.Config{Jira: c.existing}
			got := findResolvedConflict(cfg, c.newRaw, c.newResolved)
			if (got != nil) != c.wantConflict {
				t.Errorf("findResolvedConflict returned %v, wantConflict=%v", got, c.wantConflict)
			}
		})
	}
}

// TestSetupJiraConfigPathHint just covers the hint helper for
// completeness — failure modes should not be silent.
func TestSetupJiraConfigPathHint(t *testing.T) {
	if got := configPathHint(); got == "" {
		t.Error("configPathHint must return non-empty")
	}
}

// TestRunSetupJiraResolvePathFails covers the early-exit on a malformed
// override path. We use an env-var-only home with a tilde override that
// goes through expandHome, so a broken HOME forces the error path.
func TestRunSetupJiraResolvePathFails(t *testing.T) {
	// macOS / Linux let HOME be unset and UserHomeDir returns an error.
	// Setenv HOME to "" to force that path.
	t.Setenv("HOME", "")
	// Also force the XDG fallback to be unset so ResolveConfigPath has
	// to call UserHomeDir.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("JIRA_CONFIG_FILE", "")

	// Sanity: confirm UserHomeDir actually fails in this env. If not,
	// this OS auto-recovers from missing HOME and we skip.
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("OS resolves UserHomeDir without HOME; cannot exercise error path here")
	}

	err := RunSetupJira(nil)
	if err == nil {
		t.Error("expected error when home cannot be resolved")
	}
	_ = filepath.Separator // keep import to match style; used in adjacent tests
}
