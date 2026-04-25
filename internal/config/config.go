package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

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
	DefaultWorkspace WorkspaceName `yaml:"default_workspace,omitempty"`
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

// JiraConfig holds configuration for a single Atlassian site (one set of
// credentials covering one or more projects). Pigeon reads server, login,
// and auth fields from the bound jira-cli config rather than duplicating
// them here. The token is sourced from JIRA_API_TOKEN at daemon start.
type JiraConfig struct {
	JiraConfig string   `yaml:"jira_config,omitempty"` // path to jira-cli yaml; empty = default resolution chain
	Projects   []string `yaml:"projects"`              // project keys to ingest (one cursor per project)
	Account    string   `yaml:"account"`               // display name; also drives the on-disk slug via account.NameSlug
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
	Workspace    string `yaml:"workspace"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	AppToken     string `yaml:"app_token"`
	BotToken     string `yaml:"bot_token"`
	UserToken    string `yaml:"user_token,omitempty"`
	TeamID       string `yaml:"team_id"`
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

// AddJira upserts a Jira configuration entry by account label. The label is
// the unique key because it doubles as the on-disk slug; two entries with
// the same account would collide on disk.
func (c *Config) AddJira(entry JiraConfig) {
	for i, existing := range c.Jira {
		if existing.Account == entry.Account {
			c.Jira[i] = entry
			return
		}
	}
	c.Jira = append(c.Jira, entry)
}

// RemoveJira removes a Jira configuration entry by account label.
func (c *Config) RemoveJira(account string) {
	for i, existing := range c.Jira {
		if existing.Account == account {
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
