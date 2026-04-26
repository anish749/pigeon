package jira

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anish749/pigeon/internal/jira/poller"
)

const sampleCloudYAMLPigeon = `
installation: Cloud
server: https://acme.atlassian.net
login: alice@acme.com
auth_type: basic
insecure: false
project:
  key: ENG
  type: software
board:
  id: 12
  name: ENG board
epic:
  name: customfield_10011
issue:
  types:
    - name: Bug
  fields:
    custom:
      - name: Story Points
        key: customfield_10016
`

func TestLoadPigeonJiraConfigCloud(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	if err := os.WriteFile(path, []byte(sampleCloudYAMLPigeon), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadPigeonJiraConfig(path)
	if err != nil {
		t.Fatalf("LoadPigeonJiraConfig: %v", err)
	}
	if cfg.Server != "https://acme.atlassian.net" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Login != "alice@acme.com" {
		t.Errorf("Login = %q", cfg.Login)
	}
	if cfg.Project.Key != "ENG" {
		t.Errorf("Project.Key = %q", cfg.Project.Key)
	}
	if cfg.Installation != "Cloud" {
		t.Errorf("Installation = %q", cfg.Installation)
	}
	if cfg.AuthType != "basic" {
		t.Errorf("AuthType = %q", cfg.AuthType)
	}
}

func TestLoadPigeonJiraConfigLocal(t *testing.T) {
	body := `installation: Local
server: https://jira.internal.example.com
login: alice
auth_type: bearer
project:
  key: SUPPORT
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".config.yml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadPigeonJiraConfig(path)
	if err != nil {
		t.Fatalf("LoadPigeonJiraConfig: %v", err)
	}
	if cfg.APIVersion() != poller.APIVersionV2 {
		t.Errorf("APIVersion = %v, want APIVersionV2", cfg.APIVersion())
	}
}

func TestLoadPigeonJiraConfigMissingRequired(t *testing.T) {
	cases := []struct {
		name, body, missing string
	}{
		{"missing server", "login: a\nproject:\n  key: K\n", "server"},
		{"missing login", "server: https://x.atlassian.net\nproject:\n  key: K\n", "login"},
		{"missing project.key", "server: https://x.atlassian.net\nlogin: a\n", "project.key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".config.yml")
			_ = os.WriteFile(path, []byte(tc.body), 0600)
			_, err := LoadPigeonJiraConfig(path)
			if err == nil {
				t.Errorf("expected error mentioning %q", tc.missing)
			}
		})
	}
}

func TestLoadPigeonJiraConfigRefusesLocalhost(t *testing.T) {
	// Various forms of localhost should all fail. IPs are NOT refused
	// (per the design — pigeon treats 127.0.0.1, ::1, etc. as opaque
	// hosts and lets the API call decide).
	cases := []string{
		"https://localhost",
		"https://localhost:8080",
		"http://localhost/",
		"http://LOCALHOST",
	}
	for _, server := range cases {
		t.Run(server, func(t *testing.T) {
			body := "server: " + server + "\nlogin: a\nproject:\n  key: K\n"
			dir := t.TempDir()
			path := filepath.Join(dir, ".config.yml")
			_ = os.WriteFile(path, []byte(body), 0600)
			_, err := LoadPigeonJiraConfig(path)
			if err == nil {
				t.Errorf("expected localhost rejection for %q", server)
			}
		})
	}
}

func TestAccount(t *testing.T) {
	cases := []struct {
		server, wantSlug string
	}{
		{"https://acme.atlassian.net", "acme"},
		{"https://Acme.atlassian.net/", "acme"},
		{"https://jira.internal.example.com", "jira"},
		{"https://127.0.0.1", "127"},
		{"https://10.0.0.1:8080", "10"},
	}
	for _, c := range cases {
		t.Run(c.server, func(t *testing.T) {
			cfg := &PigeonJiraConfig{Server: c.server}
			acct := cfg.Account()
			if acct.Platform != "jira-issues" {
				t.Errorf("Platform = %q, want jira-issues", acct.Platform)
			}
			if acct.NameSlug() != c.wantSlug {
				t.Errorf("NameSlug() = %q, want %q", acct.NameSlug(), c.wantSlug)
			}
		})
	}
}

func TestJiraConfigBuilds(t *testing.T) {
	t.Setenv(jiraAPITokenEnv, "token-xyz")
	cfg := &PigeonJiraConfig{
		Server:   "https://acme.atlassian.net",
		Login:    "alice@acme.com",
		AuthType: "basic",
		Insecure: false,
	}
	jcfg, err := cfg.JiraConfig()
	if err != nil {
		t.Fatalf("JiraConfig: %v", err)
	}
	if jcfg.Server != cfg.Server {
		t.Errorf("Server = %q", jcfg.Server)
	}
	if jcfg.Login != cfg.Login {
		t.Errorf("Login = %q", jcfg.Login)
	}
	if jcfg.APIToken != "token-xyz" {
		t.Errorf("APIToken = %q", jcfg.APIToken)
	}
	if jcfg.AuthType == nil || string(*jcfg.AuthType) != "basic" {
		t.Errorf("AuthType = %v", jcfg.AuthType)
	}
}

func TestJiraConfigMissingToken(t *testing.T) {
	t.Setenv(jiraAPITokenEnv, "")
	cfg := &PigeonJiraConfig{
		Server:   "https://acme.atlassian.net",
		Login:    "alice@acme.com",
		AuthType: "basic",
	}
	if _, err := cfg.JiraConfig(); err == nil {
		t.Error("expected error when JIRA_API_TOKEN is unset")
	}
}

func TestJiraConfigMTLS(t *testing.T) {
	// mTLS authenticates via cert files, not the env token, so JIRA_API_TOKEN
	// is not required.
	t.Setenv(jiraAPITokenEnv, "")
	cfg := &PigeonJiraConfig{
		Server:   "https://jira.internal",
		Login:    "alice",
		AuthType: "mtls",
		MTLS: PigeonJiraMTLSConfig{
			CACert:     "/etc/ssl/ca.pem",
			ClientCert: "/etc/ssl/client.pem",
			ClientKey:  "/etc/ssl/client.key",
		},
	}
	jcfg, err := cfg.JiraConfig()
	if err != nil {
		t.Fatalf("JiraConfig: %v", err)
	}
	if jcfg.MTLSConfig.CaCert != "/etc/ssl/ca.pem" {
		t.Errorf("CaCert not propagated: %q", jcfg.MTLSConfig.CaCert)
	}
}

func TestJiraConfigMTLSIncomplete(t *testing.T) {
	t.Setenv(jiraAPITokenEnv, "")
	cfg := &PigeonJiraConfig{
		Server:   "https://jira.internal",
		Login:    "alice",
		AuthType: "mtls",
		MTLS: PigeonJiraMTLSConfig{
			CACert: "/etc/ssl/ca.pem",
			// ClientCert and ClientKey missing
		},
	}
	if _, err := cfg.JiraConfig(); err == nil {
		t.Error("expected error when mtls.client_cert and mtls.client_key are missing")
	}
}

func TestAPIVersion(t *testing.T) {
	cases := []struct {
		install string
		want    poller.APIVersion
	}{
		{"Cloud", poller.APIVersionV3},
		{"Local", poller.APIVersionV2},
		{"", poller.APIVersionV3}, // empty defaults to Cloud
	}
	for _, c := range cases {
		t.Run(c.install, func(t *testing.T) {
			cfg := &PigeonJiraConfig{Installation: c.install}
			if got := cfg.APIVersion(); got != c.want {
				t.Errorf("APIVersion(%q) = %v, want %v", c.install, got, c.want)
			}
		})
	}
}
