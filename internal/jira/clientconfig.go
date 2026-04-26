// Package jira reads a jira-cli configuration file (the YAML produced by
// `jira init`) and exposes it as a Go type pigeon can consume. Pigeon
// never owns connection settings itself — every server, login, and auth
// detail lives in the user's existing jira-cli YAML and is lifted at
// runtime via PigeonJiraConfig.
package jira

import (
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
func ResolveConfigPath(override string) string {
	if override != "" {
		return expandHome(override)
	}
	if env := os.Getenv(jiraConfigEnv); env != "" {
		return env
	}
	configHome := os.Getenv(jiraXDGConfigEnv)
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, jiraXDGSubdir)
	}
	return filepath.Join(configHome, jiraConfigSubdir, jiraConfigName)
}

// expandHome resolves a leading "~" in a path. Plain filepath.Join does
// not expand it; users put `~/...` paths in pigeon's config.yaml all the
// time, so resolve here rather than at every call site.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
