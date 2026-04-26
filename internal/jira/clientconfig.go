// Package jira reads a jira-cli configuration file (the YAML produced by
// `jira init`) and exposes it as a Go type pigeon can consume. Pigeon
// never owns connection settings itself — every server, login, and auth
// detail lives in the user's existing jira-cli YAML and is lifted at
// runtime via PigeonJiraConfig.
package jira

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// jira-cli config defaults. The constants `Dir=".jira"`, `FileName=".config"`,
// `FileType="yml"` live in jira-cli's internal/config/generator.go (and so
// can't be imported), and the config-home resolution lives in
// internal/cmdutil/utils.go: GetConfigHome returns $XDG_CONFIG_HOME if set,
// otherwise $HOME/.config. Pigeon mirrors that exact resolution so both
// tools agree on which file is bound.
const (
	jiraConfigEnv    = "JIRA_CONFIG_FILE"
	jiraXDGConfigEnv = "XDG_CONFIG_HOME"
	jiraXDGSubdir    = ".config"
	jiraConfigSubdir = ".jira"
	jiraConfigName   = ".config.yml"

	// jiraAPITokenEnv is the env var pigeon (and jira-cli) reads the API
	// token from. The token is never stored on disk.
	jiraAPITokenEnv = "JIRA_API_TOKEN"
)

// ResolveConfigPath returns the path pigeon should read for the given
// pigeon-config entry. Resolution order: explicit override → JIRA_CONFIG_FILE
// env → jira-cli default. The default itself follows jira-cli's resolution:
// $XDG_CONFIG_HOME/.jira/.config.yml if set, else $HOME/.config/.jira/.config.yml.
//
// Returns an error when the home directory is needed (no override, no
// JIRA_CONFIG_FILE, no XDG_CONFIG_HOME) but `os.UserHomeDir` fails — at
// that point the default path is undefined and silently producing a
// relative path like ".config/.jira/.config.yml" would surface later as
// a hard-to-debug "file not found" against the daemon's working dir.
func ResolveConfigPath(override string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	if env := os.Getenv(jiraConfigEnv); env != "" {
		return env, nil
	}
	configHome := os.Getenv(jiraXDGConfigEnv)
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir for default jira-cli config path: %w", err)
		}
		configHome = filepath.Join(home, jiraXDGSubdir)
	}
	return filepath.Join(configHome, jiraConfigSubdir, jiraConfigName), nil
}

// expandHome resolves a leading "~" in a path. Returns an error if "~"
// is present but `os.UserHomeDir` fails. The non-tilde fast path never
// errors.
func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: resolve home dir: %w", p, err)
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}
