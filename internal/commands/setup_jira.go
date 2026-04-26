package commands

import (
	"fmt"
	"log/slog"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	jirapkg "github.com/anish749/pigeon/internal/jira"
)

// RunSetupJira binds a jira-cli configuration to pigeon. The optional
// positional argument is a path to the jira-cli YAML; an empty string
// (no arg) means "follow the JIRA_CONFIG_FILE env / jira-cli default
// chain at runtime", which gets stored as `jira_config: ""` in pigeon's
// config so the resolution stays dynamic across env changes.
//
// The command is end-to-end: it loads the jira-cli YAML, sources the
// API token from JIRA_API_TOKEN, calls client.Me() to verify auth,
// prints the verified server/login/project, and appends to pigeon's
// config.yaml. A resolved-path conflict pre-flight catches the case
// where two entries (e.g. one explicit, one empty) would point at the
// same file — see the doc comment on config.AddJira.
func RunSetupJira(args []string) error {
	var rawPath string
	if len(args) > 0 {
		rawPath = args[0]
	}

	resolvedPath, err := jirapkg.ResolveConfigPath(rawPath)
	if err != nil {
		return fmt.Errorf("resolve jira-cli config path: %w", err)
	}

	if rawPath == "" {
		fmt.Printf("Using jira-cli config (resolved from default chain): %s\n", resolvedPath)
	} else {
		fmt.Printf("Using jira-cli config: %s\n", resolvedPath)
	}

	pjc, err := jirapkg.LoadPigeonJiraConfig(resolvedPath)
	if err != nil {
		return fmt.Errorf("%w\n\nIf you haven't set up jira-cli yet, run `jira init` first.\nSee docs/jira-protocol.md for the full setup walkthrough", err)
	}

	jcfg, err := pjc.JiraConfig()
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	client := jira.NewClient(jcfg)
	me, err := client.Me()
	if err != nil {
		return fmt.Errorf("verify auth via /myself: %w\n\nIf this is a 401, see the SSO + API token guidance in docs/jira-protocol.md", err)
	}

	acct, err := pjc.Account()
	if err != nil {
		return fmt.Errorf("derive account from server URL: %w", err)
	}

	fmt.Println()
	fmt.Println("Verified:")
	fmt.Printf("  Server:  %s\n", pjc.Server)
	fmt.Printf("  Login:   %s (%s)\n", me.Name, me.Email)
	fmt.Printf("  Project: %s\n", pjc.Project.Key)
	fmt.Printf("  Account: %s (under jira-issues/%s/)\n", acct.NameSlug(), acct.NameSlug())
	fmt.Println()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load pigeon config: %w", err)
	}

	// Resolved-path conflict check. AddJira itself dedupes on the raw
	// JiraConfig field by design (see its doc comment), so two entries
	// that resolve to the same file but differ in raw form (e.g. "" and
	// the explicit default path) would both stay. Surface that here
	// where the user can decide.
	if conflict := findResolvedConflict(cfg, rawPath, resolvedPath); conflict != nil {
		return fmt.Errorf("an existing jira entry (jira_config: %q) already resolves to %s\n\nremove it from %s first, or rerun with the same raw value to upsert it",
			conflict.JiraConfig, resolvedPath, configPathHint())
	}

	cfg.AddJira(config.JiraConfig{JiraConfig: rawPath})
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save pigeon config: %w", err)
	}

	if daemon.IsRunning() {
		fmt.Println("Saved. Daemon will pick up the entry within ~30s.")
	} else {
		fmt.Println("Saved. Run `pigeon daemon start` to begin polling.")
	}
	return nil
}

// findResolvedConflict returns an existing entry whose JiraConfig field
// resolves to the same path as the new entry but with a different raw
// string (so AddJira would see them as distinct and append rather than
// upsert). Returns nil when no conflict exists or when the conflicting
// entry has the SAME raw string (in which case AddJira's upsert is
// correct).
func findResolvedConflict(cfg *config.Config, newRaw, newResolved string) *config.JiraConfig {
	for i := range cfg.Jira {
		existing := &cfg.Jira[i]
		if existing.JiraConfig == newRaw {
			// Same raw → AddJira upserts, not a conflict.
			return nil
		}
		existingResolved, err := jirapkg.ResolveConfigPath(existing.JiraConfig)
		if err != nil {
			// Existing entry can't be resolved (e.g. broken env). Skip
			// rather than block — adding the new entry doesn't make
			// things worse.
			slog.Warn("existing jira entry cannot be resolved, skipping in conflict check",
				"jira_config", existing.JiraConfig, "err", err)
			continue
		}
		if existingResolved == newResolved {
			return existing
		}
	}
	return nil
}

// configPathHint returns a user-facing hint of the pigeon config path.
// Best-effort; if the path can't be resolved cleanly, fall back to a
// generic phrase.
func configPathHint() string {
	return "pigeon's config.yaml"
}
