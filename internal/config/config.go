package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	WhatsApp []WhatsAppConfig `yaml:"whatsapp,omitempty"`
	SlackApp *SlackApp        `yaml:"slack_app,omitempty"`
	Slack    []SlackConfig    `yaml:"slack,omitempty"`
}

type WhatsAppConfig struct {
	DeviceJID string `yaml:"device_jid"`
	DB        string `yaml:"db"`
	Account   string `yaml:"account"`
}

// SlackApp holds the app-level credentials shared across all workspaces.
type SlackApp struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	AppToken     string `yaml:"app_token"`
}

// SlackConfig holds per-workspace credentials obtained via OAuth.
type SlackConfig struct {
	Workspace string `yaml:"workspace"`
	BotToken  string `yaml:"bot_token"`
	TeamID    string `yaml:"team_id"`
}

// ConfigDir returns the config directory path.
// Respects CMU_CONFIG_DIR env var, defaults to ~/.config/cmu/
func ConfigDir() string {
	if d := os.Getenv("CMU_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cmu")
}

// ConfigPath returns the full path to config.yaml.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// Load reads the config file. Returns an empty Config if the file doesn't exist.
func Load() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", ConfigPath(), err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", ConfigPath(), err)
	}
	return &cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(ConfigPath(), data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", ConfigPath(), err)
	}
	return nil
}

// AddWhatsApp appends a WhatsApp configuration entry.
func (c *Config) AddWhatsApp(entry WhatsAppConfig) {
	c.WhatsApp = append(c.WhatsApp, entry)
}

// AddSlack appends a Slack configuration entry.
func (c *Config) AddSlack(entry SlackConfig) {
	c.Slack = append(c.Slack, entry)
}
