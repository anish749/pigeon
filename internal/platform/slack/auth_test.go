package slack

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestScopesMatchManifest guards against drift between the OAuth scopes the
// install flow requests and the scopes declared in manifests/slack-app.yaml.
// A mismatch silently breaks features like reactions for any workspace
// installed via `pigeon setup-slack`.
func TestScopesMatchManifest(t *testing.T) {
	path := filepath.Join("..", "..", "..", "manifests", "slack-app.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var manifest struct {
		OAuthConfig struct {
			Scopes struct {
				Bot  []string `yaml:"bot"`
				User []string `yaml:"user"`
			} `yaml:"scopes"`
		} `yaml:"oauth_config"`
	}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	if diff := scopeDiff(manifest.OAuthConfig.Scopes.Bot, botScopes); diff != "" {
		t.Errorf("bot scopes drift between manifest and auth.go:\n%s", diff)
	}
	if diff := scopeDiff(manifest.OAuthConfig.Scopes.User, userScopes); diff != "" {
		t.Errorf("user scopes drift between manifest and auth.go:\n%s", diff)
	}
}

func scopeDiff(manifest, code []string) string {
	m := stringSet(manifest)
	c := stringSet(code)
	var onlyManifest, onlyCode []string
	for s := range m {
		if !c[s] {
			onlyManifest = append(onlyManifest, s)
		}
	}
	for s := range c {
		if !m[s] {
			onlyCode = append(onlyCode, s)
		}
	}
	if len(onlyManifest) == 0 && len(onlyCode) == 0 {
		return ""
	}
	sort.Strings(onlyManifest)
	sort.Strings(onlyCode)
	var b strings.Builder
	if len(onlyManifest) > 0 {
		b.WriteString("  in manifest but not in auth.go: ")
		b.WriteString(strings.Join(onlyManifest, ", "))
		b.WriteString("\n")
	}
	if len(onlyCode) > 0 {
		b.WriteString("  in auth.go but not in manifest: ")
		b.WriteString(strings.Join(onlyCode, ", "))
		b.WriteString("\n")
	}
	return b.String()
}

func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
