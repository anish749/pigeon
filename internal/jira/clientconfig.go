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

// jira-cli config defaults — verified by running `jira` 1.7.0:
//
//	-c, --config string    Config file (default is /Users/anish/.config/.jira/.config.yml,
//	                       can be overridden with JIRA_CONFIG_FILE env var)
//
// The directory name has a leading dot (".jira") which is unusual but
// matches what `jira init` produces.
const (
	jiraConfigEnv    = "JIRA_CONFIG_FILE"
	jiraConfigSubdir = ".jira"
	jiraConfigName   = ".config.yml"
)

// ResolveConfigPath returns the path pigeon should read for the given
// pigeon-config entry. Resolution order: explicit override → env var →
// jira-cli default. This matches how jira-cli itself sources its config
// path, so pigeon and `jira <cmd>` always agree on which file is bound.
func ResolveConfigPath(override string) string {
	if override != "" {
		return expandHome(override)
	}
	if env := os.Getenv(jiraConfigEnv); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, jiraConfigSubdir, jiraConfigName)
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
