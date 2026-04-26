package commands

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"

	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/daemon"
	jirapkg "github.com/anish749/pigeon/internal/jira"
)

// atlassianAPITokenURL is the user-facing URL where Atlassian Cloud
// users generate API tokens. Surfaced in error messages because it is
// the single piece of guidance every first-time setup user needs.
const atlassianAPITokenURL = "https://id.atlassian.com/manage-profile/security/api-tokens"

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
		return fmt.Errorf("%w\n\nIf you haven't set up jira-cli yet, run `jira init` first to create the YAML", err)
	}

	token := os.Getenv("JIRA_API_TOKEN")
	if token == "" {
		return fmt.Errorf("JIRA_API_TOKEN is unset.\n\nGenerate an Atlassian API token at:\n  %s\nthen run:\n  export JIRA_API_TOKEN=<token>\n  pigeon setup-jira", atlassianAPITokenURL)
	}

	jcfg, err := pjc.JiraConfig(token)
	if err != nil {
		return err
	}

	client := jira.NewClient(jcfg)
	me, err := client.Me()
	if err != nil {
		return explainMeError(pjc.Server, pjc.Login, err)
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
		Account:    acct.NameSlug(),
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

// explainMeError formats a Me() failure with concrete remediation when
// the status code identifies a known cause. The frequent first-time
// failure is 401 from Atlassian Cloud sites that use SSO — the user's
// SSO password is rejected because Cloud's REST API needs an API token
// instead. We special-case that one and pass everything else through.
func explainMeError(server, login string, err error) error {
	var unexpected *jira.ErrUnexpectedResponse
	if errors.As(err, &unexpected) {
		switch unexpected.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf(
				"401 Unauthorized: %s rejected the API token for %s.\n\n"+
					"If your Atlassian site uses SSO (Okta, Google, SAML, etc.), the\n"+
					"SSO password is NOT the API token. Generate a real API token at:\n"+
					"  %s\n"+
					"then run:\n"+
					"  export JIRA_API_TOKEN=<token>\n"+
					"  pigeon setup-jira",
				server, login, atlassianAPITokenURL)
		case http.StatusForbidden:
			return fmt.Errorf(
				"403 Forbidden: %s authenticated %s but refused access to /rest/api/2/myself.\n"+
					"Confirm the user has at least 'Browse projects' permission on the bound project.",
				server, login)
		case http.StatusNotFound:
			return fmt.Errorf(
				"404 Not Found: %s does not respond to /rest/api/2/myself.\n"+
					"Confirm the `server` URL in the jira-cli config is correct.",
				server)
		}
		return fmt.Errorf("%s returned %s on /rest/api/2/myself: %s",
			server, unexpected.Status, unexpected.Body)
	}
	// Network / DNS / TLS / context errors fall through with the
	// underlying message — they already name the failure mode.
	return fmt.Errorf("%s /rest/api/2/myself: %w", server, err)
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
