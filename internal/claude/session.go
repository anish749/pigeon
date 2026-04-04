// Package claude manages Claude Code session files that bind a platform+account
// to a persistent Claude Code session ID.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/anish/claude-msg-utils/internal/daemon"
)

// Session represents a Claude Code session bound to a platform and account.
type Session struct {
	Platform      string    `yaml:"platform"`
	Account       string    `yaml:"account"`
	SessionID     string    `yaml:"session_id"`
	CWD           string    `yaml:"cwd"`
	Name          string    `yaml:"name"`
	CreatedAt     time.Time `yaml:"created_at"`
	LastDelivered time.Time `yaml:"last_delivered"`
}

// SessionsDir returns the directory where session files are stored.
func SessionsDir() string {
	return filepath.Join(daemon.StateDir(), "sessions")
}

// sessionFileName returns the file name for a platform+account pair.
// Spaces are replaced with hyphens since account names can contain spaces
// (e.g. "My Workspace" → "slack-my-workspace.yaml").
func sessionFileName(platform, account string) string {
	slug := strings.ReplaceAll(strings.ToLower(account), " ", "-")
	return fmt.Sprintf("%s-%s.yaml", strings.ToLower(platform), slug)
}

// SessionPath returns the full path to a session file for a platform+account.
func SessionPath(platform, account string) string {
	return filepath.Join(SessionsDir(), sessionFileName(platform, account))
}

// SessionName returns the display name for a session (e.g. "slack/tubular").
func SessionName(platform, account string) string {
	return fmt.Sprintf("%s/%s", strings.ToLower(platform), strings.ToLower(account))
}

// LoadSession reads a session file for the given platform+account.
// Returns nil if no session exists.
func LoadSession(platform, account string) (*Session, error) {
	data, err := os.ReadFile(SessionPath(platform, account))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session file: %w", err)
	}
	var s Session
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse session file: %w", err)
	}
	return &s, nil
}

// SaveSession writes a session file to disk.
func SaveSession(s *Session) error {
	if err := os.MkdirAll(SessionsDir(), 0755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := SessionPath(s.Platform, s.Account)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	return nil
}

// FindSession looks up an existing session by platform+account (case-insensitive).
func FindSession(platform, account string) (*Session, error) {
	return LoadSession(strings.ToLower(platform), strings.ToLower(account))
}
