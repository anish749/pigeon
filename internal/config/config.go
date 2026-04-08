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
}

// GWSConfig holds configuration for a single Google Workspace account.
type GWSConfig struct {
	Account string `yaml:"account"` // display name for this account
	Email   string `yaml:"email"`   // Google account email (used for account slug)
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
