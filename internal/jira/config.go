package jira

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
	"gopkg.in/yaml.v3"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/jira/poller"
	"github.com/anish749/pigeon/internal/paths"
)

// PigeonJiraConfig is the subset of a jira-cli YAML pigeon parses. The
// struct fields and yaml tags mirror jira-cli's on-disk schema; pigeon
// only models keys it actively consumes when constructing a
// pkg/jira.Client or routing data on disk. Fields jira-cli writes that
// pigeon does not consume (board, epic, issue.types[], issue.fields.custom,
// timezone, project.type, version) are not modeled.
//
// One pigeon entry in pigeon's config.yaml binds one PigeonJiraConfig,
// which in turn exposes one project (jira-cli configs hold a single
// `project.key` default). For a second project, the user runs
// `jira init` again with a different config path.
type PigeonJiraConfig struct {
	Server       string                  `yaml:"server"`
	Login        string                  `yaml:"login"`
	AuthType     string                  `yaml:"auth_type"`
	Insecure     bool                    `yaml:"insecure"`
	Installation string                  `yaml:"installation"`
	MTLS         PigeonJiraMTLSConfig    `yaml:"mtls"`
	Project      PigeonJiraProjectConfig `yaml:"project"`
}

// PigeonJiraMTLSConfig holds mTLS cert paths. Populated only when
// AuthType == "mtls".
type PigeonJiraMTLSConfig struct {
	CACert     string `yaml:"ca_cert"`
	ClientCert string `yaml:"client_cert"`
	ClientKey  string `yaml:"client_key"`
}

// PigeonJiraProjectConfig is jira-cli's nested project block. Only Key
// is consumed by pigeon; Type is written by jira init for its own write
// commands and ignored here.
type PigeonJiraProjectConfig struct {
	Key string `yaml:"key"`
}

// LoadPigeonJiraConfig reads a jira-cli YAML and returns the parsed
// PigeonJiraConfig. Validates that every field pigeon will pass to
// pkg/jira.NewClient or use for routing is present; returns an error
// otherwise. localhost servers are refused — they're useful for testing
// jira-cli itself but not for pigeon's ingest, which expects a real
// Atlassian site.
func LoadPigeonJiraConfig(path string) (*PigeonJiraConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read jira-cli config %s: %w", path, err)
	}
	var c PigeonJiraConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse jira-cli config %s: %w", path, err)
	}
	if c.Server == "" {
		return nil, fmt.Errorf("jira-cli config %s missing required `server` field", path)
	}
	if c.Login == "" {
		return nil, fmt.Errorf("jira-cli config %s missing required `login` field", path)
	}
	if c.Project.Key == "" {
		return nil, fmt.Errorf("jira-cli config %s missing required `project.key` field", path)
	}
	if strings.EqualFold(hostname(c.Server), "localhost") {
		return nil, fmt.Errorf("jira-cli config %s points at localhost; pigeon does not ingest localhost-bound jira instances", path)
	}
	return &c, nil
}

// Account returns the pigeon account.Account this jira-cli config maps
// to. The platform is fixed as paths.JiraPlatform and the account name
// is the lowercased first DNS label of Server. Slug derivation is owned
// by this method — callers ask for an account, not a string they have
// to feed into account.New themselves.
func (c *PigeonJiraConfig) Account() account.Account {
	return account.New(paths.JiraPlatform, c.accountSlug())
}

// accountSlug returns the lowercased first DNS label of Server, used as
// the on-disk slug under jira-issues/. Internal — exposed only via
// Account(). Slug derivation is here (rather than user-supplied) because
// jira-cli has no slug concept and pigeon should not ask the user to
// invent one.
func (c *PigeonJiraConfig) accountSlug() string {
	host := hostname(c.Server)
	if i := strings.IndexByte(host, '.'); i >= 0 {
		return strings.ToLower(host[:i])
	}
	return strings.ToLower(host)
}

// jiraAPITokenEnv is the env var pigeon (and jira-cli) reads the API
// token from. The token is never stored on disk.
const jiraAPITokenEnv = "JIRA_API_TOKEN"

// JiraConfig builds a pkg/jira.Config ready to pass to jira.NewClient.
// It owns the entire transformation: parsed YAML → token from env →
// validated MTLS paths → jira.Config. Callers don't touch env vars or
// validate auth-mode invariants themselves.
//
// Returns an error when:
//   - AuthType is not "mtls" and JIRA_API_TOKEN is unset (token-based
//     authentication needs the env var)
//   - AuthType is "mtls" and any of MTLS.{CACert, ClientCert, ClientKey}
//     is missing (mTLS authenticates via cert files, not a token, but
//     all three paths are required)
func (c *PigeonJiraConfig) JiraConfig() (jira.Config, error) {
	authType := jira.AuthType(strings.ToLower(c.AuthType))
	insecure := c.Insecure
	cfg := jira.Config{
		Server:   c.Server,
		Login:    c.Login,
		AuthType: &authType,
		Insecure: &insecure,
	}

	if authType == jira.AuthTypeMTLS {
		if c.MTLS.CACert == "" || c.MTLS.ClientCert == "" || c.MTLS.ClientKey == "" {
			return jira.Config{}, fmt.Errorf("auth_type is mtls but mtls.{ca_cert, client_cert, client_key} are not all set in jira-cli config")
		}
		cfg.MTLSConfig = jira.MTLSConfig{
			CaCert:     expandHome(c.MTLS.CACert),
			ClientCert: expandHome(c.MTLS.ClientCert),
			ClientKey:  expandHome(c.MTLS.ClientKey),
		}
		return cfg, nil
	}

	token := os.Getenv(jiraAPITokenEnv)
	if token == "" {
		return jira.Config{}, fmt.Errorf("%s env var is unset (set it to an Atlassian API token; see docs/jira-protocol.md)", jiraAPITokenEnv)
	}
	cfg.APIToken = token
	return cfg, nil
}

// APIVersion derives v3 (Cloud) vs v2 (Local / on-prem) from Installation.
// jira-cli writes the exact constants jira.InstallationTypeCloud ("Cloud")
// or jira.InstallationTypeLocal ("Local"); empty defaults to Cloud since
// that matches what `jira init` selects for a default install.
func (c *PigeonJiraConfig) APIVersion() poller.APIVersion {
	if c.Installation != "" && c.Installation != jira.InstallationTypeCloud {
		return poller.APIVersionV2
	}
	return poller.APIVersionV3
}

// hostname returns the host (without port) from a server URL, or empty
// if the URL is unparseable.
func hostname(server string) string {
	u, err := url.Parse(server)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
