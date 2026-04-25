// Package jira reads a jira-cli configuration file and builds the
// pkg/jira.Config plus API-version flag pigeon needs to construct a
// Jira REST client. Pigeon never owns these fields itself — every
// connection setting lives in the user's existing jira-cli YAML, and
// pigeon simply lifts them.
package jira

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
	"gopkg.in/yaml.v3"

	"github.com/anish749/pigeon/internal/jira/poller"
)

// jira-cli config defaults — verified by running `jira` 1.7.0:
//
//	-c, --config string    Config file (default is /Users/anish/.config/.jira/.config.yml,
//	                       can be overridden with JIRA_CONFIG_FILE env var)
//
// The directory name has a leading dot (".jira") which is unusual but
// matches what `jira init` produces.
const (
	jiraConfigEnv     = "JIRA_CONFIG_FILE"
	jiraConfigSubdir  = ".jira"
	jiraConfigName    = ".config.yml"
	jiraAPITokenEnv   = "JIRA_API_TOKEN"
	jiraInstallCloud  = "cloud"
)

// jiraCLIConfig mirrors the subset of fields pigeon reads from a
// jira-cli YAML. Discovered fields written by `jira init` (project.key,
// board, epic.*, issue.types, issue.fields.custom) are ignored — they
// exist for jira-cli's write commands and do not affect read-only ingest.
type jiraCLIConfig struct {
	Installation string `yaml:"installation"`
	Server       string `yaml:"server"`
	Login        string `yaml:"login"`
	AuthType     string `yaml:"auth_type"`
	Insecure     bool   `yaml:"insecure"`
	MTLS         struct {
		CACert     string `yaml:"ca_cert"`
		ClientCert string `yaml:"client_cert"`
		ClientKey  string `yaml:"client_key"`
	} `yaml:"mtls"`
}

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

// LoadClientConfig reads a jira-cli config from the given path, sources
// the API token from JIRA_API_TOKEN env, and returns a jira.Config ready
// to pass to jira.NewClient plus the APIVersion that selects v3 vs v2
// endpoint dispatch in the poller.
//
// Returns an error if the file is missing, malformed, or the env token is
// unset — the daemon should disable Jira ingest in that case rather than
// loop with auth failures.
func LoadClientConfig(path string) (jira.Config, poller.APIVersion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return jira.Config{}, 0, fmt.Errorf("read jira-cli config %s: %w", path, err)
	}
	var c jiraCLIConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return jira.Config{}, 0, fmt.Errorf("parse jira-cli config %s: %w", path, err)
	}

	token := os.Getenv(jiraAPITokenEnv)
	if token == "" {
		return jira.Config{}, 0, fmt.Errorf("%s env var is unset (set it to an Atlassian API token; see docs/jira-protocol.md)", jiraAPITokenEnv)
	}
	if c.Server == "" {
		return jira.Config{}, 0, fmt.Errorf("jira-cli config %s missing required `server` field", path)
	}
	if c.Login == "" {
		return jira.Config{}, 0, fmt.Errorf("jira-cli config %s missing required `login` field", path)
	}

	// jira-cli writes "Cloud" (capitalized) but treat any case as cloud.
	// Default to cloud when unset since that is by far the more common case
	// in 2026; on-prem users should have explicitly set installation: local.
	apiVer := poller.APIVersionV3
	if !strings.EqualFold(c.Installation, "") && !strings.EqualFold(c.Installation, jiraInstallCloud) {
		apiVer = poller.APIVersionV2
	}

	authType := jira.AuthType(strings.ToLower(c.AuthType))
	insecure := c.Insecure

	jcfg := jira.Config{
		Server:   c.Server,
		Login:    c.Login,
		APIToken: token,
		AuthType: &authType,
		Insecure: &insecure,
	}
	if c.MTLS.CACert != "" {
		jcfg.MTLSConfig = jira.MTLSConfig{
			CaCert:     expandHome(c.MTLS.CACert),
			ClientCert: expandHome(c.MTLS.ClientCert),
			ClientKey:  expandHome(c.MTLS.ClientKey),
		}
	}

	return jcfg, apiVer, nil
}
