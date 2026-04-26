package commands

import (
	"fmt"
	"os"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	jirapkg "github.com/anish749/pigeon/internal/jira"
)

// RunSetupJira binds a jira-cli configuration to pigeon. The optional
// positional argument is a path to the jira-cli YAML; with no arg, the
// path is resolved via the JIRA_CONFIG_FILE env / jira-cli default
// chain at setup time. Either way the resolved absolute path is what
// gets persisted.
//
// The command is end-to-end:
//
//  1. Resolve the path.
//  2. Load the jira-cli YAML and validate required fields.
//  3. Source JIRA_API_TOKEN from env (this is its only role —
//     persistence happens in step 6).
//  4. Call client.Me() to verify auth.
//  5. Print the verified server / login / project / account.
//  6. Upsert a config.JiraConfig{JiraConfig: <resolved>, APIToken: <env>}
//     entry, keyed by resolved path.
//
// After setup the daemon doesn't need any env var — both the path and
// the token live in pigeon's config. Step 6 upserts so re-running setup
// after a token rotation just refreshes the stored token.
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
		fmt.Printf("Resolving jira-cli config from default chain: %s\n", resolvedPath)
	} else {
		fmt.Printf("Using jira-cli config: %s\n", resolvedPath)
	}

	pjc, err := jirapkg.LoadPigeonJiraConfig(resolvedPath)
	if err != nil {
		return fmt.Errorf("%w\n\nIf you haven't set up jira-cli yet, run `jira init` first.\nSee docs/jira-protocol.md for the full setup walkthrough", err)
	}

	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		return fmt.Errorf("JIRA_API_TOKEN env var is unset (export an Atlassian API token before running setup-jira; see docs/jira-protocol.md)")
	}

	jcfg, err := pjc.JiraConfig(token)
	if err != nil {
		return err
	}

	client := jira.NewClient(jcfg)
	me, err := client.Me()
	if err != nil {
		return fmt.Errorf("verify auth via GET %s/rest/api/2/myself: %w\n\nIf this is a 401, see the SSO + API token guidance in docs/jira-protocol.md", pjc.Server, err)
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

	entry := config.JiraConfig{
		JiraConfig: resolvedPath,
		APIToken:   token,
	}
	upsertJiraByResolvedPath(cfg, entry, resolvedPath)

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

// upsertJiraByResolvedPath inserts entry into cfg.Jira, replacing any
// existing entry whose JiraConfig field resolves to resolvedPath
// (regardless of raw form). This is setup-jira's idempotency guarantee:
// running it twice with the same path produces the same single entry,
// even if the first call wrote `jira_config: ""` and the second wrote
// `jira_config: /Users/anish/.config/.jira/.config.yml`. config.AddJira
// keys on the raw field for hand-edit semantics; setup-jira always
// writes a canonical resolved form, so its idempotency lives here.
func upsertJiraByResolvedPath(cfg *config.Config, entry config.JiraConfig, resolvedPath string) {
	for i := range cfg.Jira {
		existing := &cfg.Jira[i]
		existingResolved, err := jirapkg.ResolveConfigPath(existing.JiraConfig)
		if err != nil {
			continue
		}
		if existingResolved == resolvedPath {
			cfg.Jira[i] = entry
			return
		}
	}
	cfg.Jira = append(cfg.Jira, entry)
}
