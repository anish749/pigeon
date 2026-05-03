package daemon

import (
	"sort"
	"testing"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
)

// TestConfiguredAccounts verifies the config → account list mapping covers
// every platform the daemon manages and does not silently drop one.
func TestConfiguredAccounts(t *testing.T) {
	cfg := &config.Config{
		Slack:    []config.SlackConfig{{Workspace: "acme"}},
		GWS:      []config.GWSConfig{{Email: "user@example.com"}},
		WhatsApp: []config.WhatsAppConfig{{Account: "+15551234567"}},
		Linear:   []config.LinearConfig{{Workspace: "my-team"}},
		Jira:     []config.JiraConfig{{AccountName: "atlassian"}},
	}

	got := configuredAccounts(cfg)
	gotKeys := make([]string, 0, len(got))
	for _, a := range got {
		gotKeys = append(gotKeys, a.String())
	}
	sort.Strings(gotKeys)

	want := []account.Account{
		account.New("slack", "acme"),
		account.New("gws", "user@example.com"),
		account.New("whatsapp", "+15551234567"),
		account.New("linear", "my-team"),
		account.New("jira-issues", "atlassian"),
	}
	wantKeys := make([]string, 0, len(want))
	for _, a := range want {
		wantKeys = append(wantKeys, a.String())
	}
	sort.Strings(wantKeys)

	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("got %d accounts (%v), want %d (%v)", len(gotKeys), gotKeys, len(wantKeys), wantKeys)
	}
	for i := range gotKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Errorf("account %d: got %q, want %q", i, gotKeys[i], wantKeys[i])
		}
	}
}

// TestConfiguredAccounts_EmptyConfig verifies the empty case returns nil
// without panicking.
func TestConfiguredAccounts_EmptyConfig(t *testing.T) {
	got := configuredAccounts(&config.Config{})
	if len(got) != 0 {
		t.Errorf("got %d accounts for empty config, want 0", len(got))
	}
}
