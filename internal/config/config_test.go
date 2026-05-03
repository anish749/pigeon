package config

import (
	"sort"
	"testing"

	"github.com/anish749/pigeon/internal/account"
)

// TestAllAccounts verifies the config → account list mapping covers every
// platform and does not silently drop one. This is the single canonical
// place where Config is unrolled into account.Account values; callers
// (workspace resolution, daemon maintenance scheduler) all go through it.
func TestAllAccounts(t *testing.T) {
	cfg := &Config{
		Slack:    []SlackConfig{{Workspace: "acme"}},
		GWS:      []GWSConfig{{Email: "user@example.com"}},
		WhatsApp: []WhatsAppConfig{{Account: "+15551234567"}},
		Linear:   []LinearConfig{{Workspace: "my-team"}},
		Jira:     []JiraConfig{{AccountName: "atlassian"}},
	}

	got := cfg.AllAccounts()
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
		account.New("jira", "atlassian"),
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

// TestAllAccounts_EmptyConfig verifies the empty case returns nil without
// panicking.
func TestAllAccounts_EmptyConfig(t *testing.T) {
	got := (&Config{}).AllAccounts()
	if len(got) != 0 {
		t.Errorf("got %d accounts for empty config, want 0", len(got))
	}
}
