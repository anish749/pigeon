package pctx

import (
	"testing"

	"github.com/anish749/pigeon/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{
		GWS: []config.GWSConfig{
			{Account: "work", Email: "work@company.com"},
			{Account: "personal", Email: "user@gmail.com"},
		},
		Slack: []config.SlackConfig{
			{Workspace: "acme-corp", TeamID: "T01"},
			{Workspace: "side-project", TeamID: "T02"},
		},
		WhatsApp: []config.WhatsAppConfig{
			{Account: "+15551234567", DeviceJID: "15551234567:0@s.whatsapp.net"},
		},
		Linear: []config.LinearConfig{
			{Workspace: "trudy", Account: "trudy"},
		},
		Contexts: map[string]config.ContextConfig{
			"work": {
				GWS:   []string{"work@company.com"},
				Slack: []string{"acme-corp"},
			},
			"personal": {
				GWS:      []string{"user@gmail.com"},
				Slack:    []string{"side-project"},
				WhatsApp: []string{"+15551234567"},
			},
			"all-slack": {
				Slack: []string{"acme-corp", "side-project"},
			},
		},
		DefaultContext: "personal",
	}
}

// --- ResolveContextName tests ---

func TestResolveContextNameFlagWins(t *testing.T) {
	cfg := testConfig()
	got := ResolveContextName("work", "personal", cfg)
	if got != "work" {
		t.Errorf("got %q, want %q", got, "work")
	}
}

func TestResolveContextNameEnvOverridesDefault(t *testing.T) {
	cfg := testConfig()
	got := ResolveContextName("", "work", cfg)
	if got != "work" {
		t.Errorf("got %q, want %q", got, "work")
	}
}

func TestResolveContextNameFallsBackToDefault(t *testing.T) {
	cfg := testConfig()
	got := ResolveContextName("", "", cfg)
	if got != "personal" {
		t.Errorf("got %q, want %q", got, "personal")
	}
}

func TestResolveContextNameEmpty(t *testing.T) {
	cfg := &config.Config{}
	got := ResolveContextName("", "", cfg)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// --- Resolve tests ---

func TestResolveWithContext(t *testing.T) {
	cfg := testConfig()

	res, err := Resolve(cfg, SourceGmail, ResolveOpts{Context: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContextName != "work" {
		t.Errorf("context = %q, want %q", res.ContextName, "work")
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Name != "work@company.com" {
		t.Errorf("accounts = %v, want [work@company.com]", res.Accounts)
	}
}

func TestResolveWithDefaultContext(t *testing.T) {
	cfg := testConfig()

	// Simulate what the CLI does: resolve context name first, then pass it.
	ctxName := ResolveContextName("", "", cfg)
	res, err := Resolve(cfg, SourceSlack, ResolveOpts{Context: ctxName})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContextName != "personal" {
		t.Errorf("context = %q, want %q", res.ContextName, "personal")
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Name != "side-project" {
		t.Errorf("accounts = %v, want [side-project]", res.Accounts)
	}
}

func TestResolveAccountBypass(t *testing.T) {
	cfg := testConfig()

	res, err := Resolve(cfg, SourceGmail, ResolveOpts{Account: "work@company.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ContextName != "" {
		t.Errorf("context = %q, want empty", res.ContextName)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Name != "work@company.com" {
		t.Errorf("accounts = %v, want [work@company.com]", res.Accounts)
	}
}

func TestResolveMultipleAccounts(t *testing.T) {
	cfg := testConfig()

	res, err := Resolve(cfg, SourceSlack, ResolveOpts{Context: "all-slack"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(res.Accounts))
	}
}

func TestResolveNoAccountForSource(t *testing.T) {
	cfg := testConfig()

	_, err := Resolve(cfg, SourceWhatsApp, ResolveOpts{Context: "work"})
	if err == nil {
		t.Fatal("expected error for missing whatsapp in work context")
	}
}

func TestResolveUnknownContext(t *testing.T) {
	cfg := testConfig()

	_, err := Resolve(cfg, SourceGmail, ResolveOpts{Context: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown context")
	}
}

func TestResolveWithoutContextSingleAccount(t *testing.T) {
	cfg := &config.Config{
		GWS: []config.GWSConfig{
			{Account: "work", Email: "work@company.com"},
		},
	}

	// No context passed, only one GWS account — infer.
	res, err := Resolve(cfg, SourceGmail, ResolveOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Name != "work@company.com" {
		t.Errorf("accounts = %v, want [work@company.com]", res.Accounts)
	}
}

func TestResolveWithoutContextAmbiguous(t *testing.T) {
	cfg := &config.Config{
		GWS: []config.GWSConfig{
			{Account: "work", Email: "work@company.com"},
			{Account: "personal", Email: "user@gmail.com"},
		},
	}

	_, err := Resolve(cfg, SourceGmail, ResolveOpts{})
	if err == nil {
		t.Fatal("expected error for ambiguous accounts without context")
	}
}

func TestResolveWhatsAppByPhone(t *testing.T) {
	cfg := testConfig()

	res, err := Resolve(cfg, SourceWhatsApp, ResolveOpts{Context: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].Name != "+15551234567" {
		t.Errorf("accounts = %v, want [+15551234567]", res.Accounts)
	}
}

func TestMatchWhatsAppPhone(t *testing.T) {
	tests := []struct {
		jid, phone string
		want       bool
	}{
		{"15551234567:0@s.whatsapp.net", "+15551234567", true},
		{"15551234567:0@s.whatsapp.net", "15551234567", true},
		{"15551234567:0@s.whatsapp.net", "+19999999999", false},
		{"15551234567:0@s.whatsapp.net", "", false},
	}
	for _, tt := range tests {
		if got := matchWhatsAppPhone(tt.jid, tt.phone); got != tt.want {
			t.Errorf("matchWhatsAppPhone(%q, %q) = %v, want %v", tt.jid, tt.phone, got, tt.want)
		}
	}
}

func TestParseSource(t *testing.T) {
	for _, name := range []string{"gmail", "calendar", "drive", "slack", "whatsapp", "linear"} {
		src, err := ParseSource(name)
		if err != nil {
			t.Errorf("ParseSource(%q) error: %v", name, err)
		}
		if string(src) != name {
			t.Errorf("ParseSource(%q) = %q", name, src)
		}
	}

	_, err := ParseSource("gcalendar")
	if err == nil {
		t.Error("expected error for unknown source")
	}
}

func TestSourcePlatform(t *testing.T) {
	tests := []struct {
		src  Source
		want Platform
	}{
		{SourceGmail, PlatformGWS},
		{SourceCalendar, PlatformGWS},
		{SourceDrive, PlatformGWS},
		{SourceSlack, PlatformSlack},
		{SourceWhatsApp, PlatformWhatsApp},
		{SourceLinear, PlatformLinear},
	}
	for _, tt := range tests {
		if got := tt.src.Platform(); got != tt.want {
			t.Errorf("%s.Platform() = %q, want %q", tt.src, got, tt.want)
		}
	}
}
