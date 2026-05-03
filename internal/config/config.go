package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
)

type Config struct {
	WhatsApp []WhatsAppConfig `yaml:"whatsapp,omitempty"`
	Slack    []SlackConfig    `yaml:"slack,omitempty"`
	GWS      []GWSConfig      `yaml:"gws,omitempty"`
	Linear   []LinearConfig   `yaml:"linear,omitempty"`
	Jira     []JiraConfig     `yaml:"jira,omitempty"`

	// Workspaces define named account groupings. When a workspace is active,
	// identity resolution and reads are scoped to that workspace's accounts.
	// When no workspace is set, all accounts are visible and identity is
	// merged across every known source.
	Workspaces       map[WorkspaceName]WorkspaceConfig `yaml:"workspaces,omitempty"`
	DefaultWorkspace WorkspaceName                     `yaml:"default_workspace,omitempty"`
}

// WorkspaceName is the resolved name of the active workspace.
type WorkspaceName string

// WorkspaceConfig lists the accounts that belong to a named workspace. Each
// field holds account slugs (the same slug used in storage paths:
// workspace slug for Slack, email slug for GWS, account slug for WhatsApp).
type WorkspaceConfig struct {
	Slack    []string `yaml:"slack,omitempty"`
	GWS      []string `yaml:"gws,omitempty"`
	WhatsApp []string `yaml:"whatsapp,omitempty"`
	Linear   []string `yaml:"linear,omitempty"`
	Jira     []string `yaml:"jira,omitempty"`
}

// LinearConfig holds configuration for a single Linear workspace.
type LinearConfig struct {
	Workspace string `yaml:"workspace"` // Linear workspace slug
	Account   string `yaml:"account"`   // display name for pigeon
}

// JiraConfig binds one jira-cli configuration to pigeon's ingest.
//
// All three fields are populated by `pigeon setup-jira` at install
// time and snapshot the state that was verified end-to-end. The daemon
// reads them as-is at startup; no environment variable, default-path
// chain, or tilde expansion runs at runtime.
//
// AccountName is the lowercased first DNS label of the bound YAML's
// server URL, captured at setup time. Persisting it lets the daemon
// and workspace machinery resolve the account.Account without
// reopening the jira-cli YAML — see the Account method.
//
// Server / login / auth / project still come from the bound jira-cli
// YAML, not duplicated here.
type JiraConfig struct {
	JiraConfig  string `yaml:"jira_config"` // absolute path to jira-cli yaml
	APIToken    string `yaml:"api_token"`   // Atlassian API token
	AccountName string `yaml:"account"`     // first DNS label of server URL, captured at setup
}

// Account returns the account.Account this entry maps to. The
// platform is fixed as paths.JiraPlatform and the name is the
// AccountName captured by setup-jira; constructing it here keeps
// callers (daemon, workspace) from having to know the platform
// constant or replicate account.New wiring.
func (j JiraConfig) Account() account.Account {
	return account.New(paths.JiraPlatform, j.AccountName)
}

// GWSConfig holds configuration for a single Google Workspace account.
type GWSConfig struct {
	Account string            `yaml:"account"` // display name for this account
	Email   string            `yaml:"email"`   // Google account email (used for account slug)
	Env     map[string]string `yaml:"env,omitempty"`
}

type WhatsAppConfig struct {
	DeviceJID string `yaml:"device_jid"`
	DB        string `yaml:"db"`
	Account   string `yaml:"account"`
}

// SlackConfig holds all credentials for a single workspace.
// Each workspace has its own internal Slack app for full rate limits.
type SlackConfig struct {
	Workspace      string `yaml:"workspace"`
	AppDisplayName string `yaml:"app_display_name,omitempty"`
	ClientID       string `yaml:"client_id"`
	ClientSecret   string `yaml:"client_secret"`
	AppToken       string `yaml:"app_token"`
	BotToken       string `yaml:"bot_token"`
	UserToken      string `yaml:"user_token,omitempty"`
	TeamID         string `yaml:"team_id"`
}

// AppDisplay returns the proper-noun form: manifest name, bot display name,
// listener auto-reply. Defaults to "Pigeon".
func (s SlackConfig) AppDisplay() string {
	if s.AppDisplayName != "" {
		return s.AppDisplayName
	}
	return "Pigeon"
}

// AppAttribution returns the sentence-case form: "sent via X" footer.
// Defaults to lowercase "pigeon".
func (s SlackConfig) AppAttribution() string {
	if s.AppDisplayName != "" {
		return s.AppDisplayName
	}
	return "pigeon"
}

// AllAccounts returns every account.Account derivable from this config
// across all platforms. The single canonical place where the cross-
// platform shape of Config is unrolled into the per-account model that
// the rest of the daemon (workspace resolution, maintenance scheduler,
// list filters) operates on.
func (c *Config) AllAccounts() []account.Account {
	var accounts []account.Account
	for _, s := range c.Slack {
		accounts = append(accounts, account.New("slack", s.Workspace))
	}
	for _, g := range c.GWS {
		accounts = append(accounts, account.New("gws", g.Email))
	}
	for _, w := range c.WhatsApp {
		accounts = append(accounts, account.New("whatsapp", w.Account))
	}
	for _, l := range c.Linear {
		accounts = append(accounts, account.New("linear", l.Workspace))
	}
	for _, j := range c.Jira {
		accounts = append(accounts, j.Account())
	}
	return accounts
}

// Load reads the config file. Returns an empty Config if the file doesn't exist.
func Load() (*Config, error) {
	data, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", paths.ConfigPath(), err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", paths.ConfigPath(), err)
	}
	return &cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	if err := os.MkdirAll(paths.ConfigDir(), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(paths.ConfigPath(), data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", paths.ConfigPath(), err)
	}
	return nil
}

// AddWhatsApp upserts a WhatsApp configuration entry by account.
func (c *Config) AddWhatsApp(entry WhatsAppConfig) {
	for i, existing := range c.WhatsApp {
		if existing.Account == entry.Account {
			c.WhatsApp[i] = entry
			return
		}
	}
	c.WhatsApp = append(c.WhatsApp, entry)
}

// RemoveWhatsApp removes a WhatsApp configuration entry by account.
func (c *Config) RemoveWhatsApp(account string) {
	for i, existing := range c.WhatsApp {
		if existing.Account == account {
			c.WhatsApp = append(c.WhatsApp[:i], c.WhatsApp[i+1:]...)
			return
		}
	}
}

// RemoveSlack removes a Slack configuration entry by workspace name.
func (c *Config) RemoveSlack(workspace string) {
	for i, existing := range c.Slack {
		if existing.Workspace == workspace {
			c.Slack = append(c.Slack[:i], c.Slack[i+1:]...)
			return
		}
	}
}

// AddSlack upserts a Slack configuration entry by team ID.
// If a workspace with the same team ID already exists, it is overwritten.
func (c *Config) AddSlack(entry SlackConfig) {
	for i, existing := range c.Slack {
		if existing.TeamID == entry.TeamID {
			c.Slack[i] = entry
			return
		}
	}
	c.Slack = append(c.Slack, entry)
}

// AddLinear upserts a Linear configuration entry by workspace.
func (c *Config) AddLinear(entry LinearConfig) {
	for i, existing := range c.Linear {
		if existing.Workspace == entry.Workspace {
			c.Linear[i] = entry
			return
		}
	}
	c.Linear = append(c.Linear, entry)
}

// AddJira upserts a Jira configuration entry by the JiraConfig field as
// the user typed it — a literal string compare, NOT a resolved-path
// compare. An empty string and an explicit path that happen to resolve
// to the same file are treated as distinct entries on purpose: the
// empty sentinel means "follow the JIRA_CONFIG_FILE env / default
// chain at runtime" and an explicit path is a pinned override; collapsing
// them would erase that intent.
//
// At runtime, JiraManager.reconcile resolves each entry via
// ResolveConfigPath and dedupes the resulting paths via a map, so two
// entries that resolve to the same file produce only one poller — the
// hand-edited config can carry redundant-looking entries without
// corrupting ingest.
//
// Resolved-path conflict detection ("you already have an entry pointing
// at this file") belongs in `pigeon setup jira` rather than here: only
// the setup command has the user's attention and an interactive prompt
// to decide what to keep.
func (c *Config) AddJira(entry JiraConfig) {
	for i, existing := range c.Jira {
		if existing.JiraConfig == entry.JiraConfig {
			c.Jira[i] = entry
			return
		}
	}
	c.Jira = append(c.Jira, entry)
}

// RemoveJira removes a Jira configuration entry by jira-cli config path.
func (c *Config) RemoveJira(jiraConfig string) {
	for i, existing := range c.Jira {
		if existing.JiraConfig == jiraConfig {
			c.Jira = append(c.Jira[:i], c.Jira[i+1:]...)
			return
		}
	}
}

// AddGWS upserts a GWS configuration entry by email.
func (c *Config) AddGWS(entry GWSConfig) {
	for i, existing := range c.GWS {
		if existing.Email == entry.Email {
			c.GWS[i] = entry
			return
		}
	}
	c.GWS = append(c.GWS, entry)
}
