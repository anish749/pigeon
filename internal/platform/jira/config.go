package jira

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	jira "github.com/ankitpokhrel/jira-cli/pkg/jira"
	"gopkg.in/yaml.v3"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/platform/jira/poller"
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
	host, err := serverHostname(c.Server)
	if err != nil {
		return nil, fmt.Errorf("jira-cli config %s: %w", path, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil, fmt.Errorf("jira-cli config %s points at localhost; pigeon does not ingest localhost-bound jira instances", path)
	}
	return &c, nil
}

// Account returns the pigeon account.Account this jira-cli config maps
// to. The platform is fixed as paths.JiraPlatform and the account name
// is the lowercased first DNS label of Server. Returns an error if
// Server is malformed (no scheme, unparseable URL, or empty hostname).
// LoadPigeonJiraConfig validates Server at load time, so callers using
// Load won't see this error in practice — but the error path is real
// because direct PigeonJiraConfig construction (e.g. in tests) bypasses
// that validation.
func (c *PigeonJiraConfig) Account() (account.Account, error) {
	host, err := serverHostname(c.Server)
	if err != nil {
		return account.Account{}, err
	}
	if i := strings.IndexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	return account.New(paths.JiraPlatform, strings.ToLower(host)), nil
}

// JiraConfig builds a pkg/jira.Config from the parsed YAML and the
// caller-supplied API token. The token comes from pigeon's persisted
// config (populated by setup-jira) — sourcing it is the caller's
// concern, not this method's.
//
// Returns an error when:
//   - AuthType is not "mtls" and token is empty
//   - AuthType is "mtls" and any of MTLS.{CACert, ClientCert, ClientKey}
//     is missing (mTLS authenticates via cert files, not a token, but
//     all three paths are required)
func (c *PigeonJiraConfig) JiraConfig(token string) (jira.Config, error) {
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
		caCert, err := expandHome(c.MTLS.CACert)
		if err != nil {
			return jira.Config{}, fmt.Errorf("expand mtls.ca_cert: %w", err)
		}
		clientCert, err := expandHome(c.MTLS.ClientCert)
		if err != nil {
			return jira.Config{}, fmt.Errorf("expand mtls.client_cert: %w", err)
		}
		clientKey, err := expandHome(c.MTLS.ClientKey)
		if err != nil {
			return jira.Config{}, fmt.Errorf("expand mtls.client_key: %w", err)
		}
		cfg.MTLSConfig = jira.MTLSConfig{
			CaCert:     caCert,
			ClientCert: clientCert,
			ClientKey:  clientKey,
		}
		return cfg, nil
	}

	if token == "" {
		return jira.Config{}, fmt.Errorf("api token is empty (run `pigeon setup-jira` to populate it)")
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

// serverHostname extracts the hostname (no port) from a server URL.
// Returns an error if the URL is malformed or has no hostname — note
// that "acme.atlassian.net" without a scheme parses cleanly via
// url.Parse but produces an empty Host, so the second case is a real
// failure mode and not just paranoia.
func serverHostname(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse server URL %q: %w", server, err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("server URL %q has no hostname (missing scheme like https://?)", server)
	}
	return host, nil
}
