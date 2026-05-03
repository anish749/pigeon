// Package workspace resolves the active workspace from config, environment,
// and CLI flags. A workspace is a named set of accounts that scopes all
// pigeon commands. See docs/read-protocol.md.
package workspace

import (
	"fmt"
	"os"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
)

// EnvWorkspace is the environment variable that sets the active workspace.
const EnvWorkspace = "PIGEON_WORKSPACE"

// Workspace is the resolved active workspace with its accounts.
type Workspace struct {
	Name     config.WorkspaceName
	Accounts []account.Account
}

// GetCurrentWorkspace determines the active workspace and resolves its accounts.
//
// Resolution order:
//  1. flagOverride (--workspace CLI flag)
//  2. PIGEON_WORKSPACE environment variable
//  3. default_workspace in config
//  4. No workspace — all configured accounts
func GetCurrentWorkspace(cfg *config.Config, flagOverride string) (*Workspace, error) {
	if flagOverride != "" {
		return resolve(cfg, config.WorkspaceName(flagOverride), "--workspace flag")
	}
	if env := os.Getenv(EnvWorkspace); env != "" {
		return resolve(cfg, config.WorkspaceName(env), EnvWorkspace)
	}
	if cfg.DefaultWorkspace != "" {
		return resolve(cfg, cfg.DefaultWorkspace, "default_workspace in config")
	}
	return &Workspace{Accounts: cfg.AllAccounts()}, nil
}

// resolve looks up a workspace by name and maps its account slugs to
// concrete account.Account values.
func resolve(cfg *config.Config, ws config.WorkspaceName, source string) (*Workspace, error) {
	wsCfg, ok := cfg.Workspaces[ws]
	if !ok {
		return nil, fmt.Errorf("unknown workspace %q (%s)", ws, source)
	}
	var accounts []account.Account
	for _, slug := range wsCfg.Slack {
		accounts = append(accounts, account.New("slack", slug))
	}
	for _, slug := range wsCfg.GWS {
		accounts = append(accounts, account.New("gws", slug))
	}
	for _, slug := range wsCfg.WhatsApp {
		accounts = append(accounts, account.New("whatsapp", slug))
	}
	for _, slug := range wsCfg.Linear {
		accounts = append(accounts, account.New("linear", slug))
	}
	for _, slug := range wsCfg.Jira {
		accounts = append(accounts, account.New(paths.JiraPlatform, slug))
	}
	return &Workspace{Name: ws, Accounts: accounts}, nil
}

// GetAllWorkspaces returns every named workspace in the config, resolved with
// their accounts. Returns an error if any workspace fails to resolve.
func GetAllWorkspaces(cfg *config.Config) ([]*Workspace, error) {
	var workspaces []*Workspace
	for name := range cfg.Workspaces {
		ws, err := resolve(cfg, name, "config")
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}

// Contains reports whether the workspace includes the given account.
func (w *Workspace) Contains(acct account.Account) bool {
	for _, a := range w.Accounts {
		if a.Platform == acct.Platform && a.NameSlug() == acct.NameSlug() {
			return true
		}
	}
	return false
}

// IsConfigured reports whether acct is present anywhere in cfg.
// Comparison matches Workspace.Contains — equality on Platform and
// NameSlug — so a slug-equivalent display name resolves the same as it
// does inside a workspace.
func IsConfigured(cfg *config.Config, acct account.Account) bool {
	for _, a := range cfg.AllAccounts() {
		if a.Platform == acct.Platform && a.NameSlug() == acct.NameSlug() {
			return true
		}
	}
	return false
}

// AccountsForPlatform returns the subset of workspace accounts matching the
// given platform. Returns all accounts if platform is empty.
func (w *Workspace) AccountsForPlatform(platform string) []account.Account {
	if platform == "" {
		return w.Accounts
	}
	var out []account.Account
	for _, a := range w.Accounts {
		if a.Platform == platform {
			out = append(out, a)
		}
	}
	return out
}

