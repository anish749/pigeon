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

func TestResolveContextName(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		envContext string
		cfg        *config.Config
		want       ContextName
	}{
		{"flag wins over env and default", "work", "personal", testConfig(), "work"},
		{"env overrides default", "", "work", testConfig(), "work"},
		{"falls back to default", "", "", testConfig(), "personal"},
		{"empty when no default", "", "", &config.Config{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveContextName(tt.flag, tt.envContext, tt.cfg)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.Config
		src         Source
		opts        ResolveOpts
		wantCtx     ContextName
		wantAccts   []string // expected account names
		wantErr     bool
	}{
		{
			name:      "context selects accounts for source platform",
			cfg:       testConfig(),
			src:       SourceGmail,
			opts:      ResolveOpts{Context: "work"},
			wantCtx:   "work",
			wantAccts: []string{"work@company.com"},
		},
		{
			name: "default context via ResolveContextName",
			cfg:  testConfig(),
			src:  SourceSlack,
			// Simulate CLI: resolve context name first, then pass it.
			opts:      ResolveOpts{Context: ResolveContextName("", "", testConfig())},
			wantCtx:   "personal",
			wantAccts: []string{"side-project"},
		},
		{
			name:      "account flag bypasses context",
			cfg:       testConfig(),
			src:       SourceGmail,
			opts:      ResolveOpts{Account: "work@company.com"},
			wantCtx:   "",
			wantAccts: []string{"work@company.com"},
		},
		{
			name:      "multiple accounts in context",
			cfg:       testConfig(),
			src:       SourceSlack,
			opts:      ResolveOpts{Context: "all-slack"},
			wantCtx:   "all-slack",
			wantAccts: []string{"acme-corp", "side-project"},
		},
		{
			name:      "whatsapp matched by phone",
			cfg:       testConfig(),
			src:       SourceWhatsApp,
			opts:      ResolveOpts{Context: "personal"},
			wantCtx:   "personal",
			wantAccts: []string{"+15551234567"},
		},
		{
			name: "single account inferred without context",
			cfg: &config.Config{
				GWS: []config.GWSConfig{{Account: "work", Email: "work@company.com"}},
			},
			src:       SourceGmail,
			opts:      ResolveOpts{},
			wantAccts: []string{"work@company.com"},
		},
		{
			name:    "no account for source in context",
			cfg:     testConfig(),
			src:     SourceWhatsApp,
			opts:    ResolveOpts{Context: "work"},
			wantErr: true,
		},
		{
			name:    "unknown context",
			cfg:     testConfig(),
			src:     SourceGmail,
			opts:    ResolveOpts{Context: "nonexistent"},
			wantErr: true,
		},
		{
			name: "ambiguous without context",
			cfg: &config.Config{
				GWS: []config.GWSConfig{
					{Account: "work", Email: "work@company.com"},
					{Account: "personal", Email: "user@gmail.com"},
				},
			},
			src:     SourceGmail,
			opts:    ResolveOpts{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Resolve(tt.cfg, tt.src, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.ContextName != tt.wantCtx {
				t.Errorf("context = %q, want %q", res.ContextName, tt.wantCtx)
			}
			if len(res.Accounts) != len(tt.wantAccts) {
				t.Fatalf("got %d accounts, want %d", len(res.Accounts), len(tt.wantAccts))
			}
			for i, want := range tt.wantAccts {
				if res.Accounts[i].Name != want {
					t.Errorf("account[%d] = %q, want %q", i, res.Accounts[i].Name, want)
				}
			}
		})
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
	valid := []string{"gmail", "calendar", "drive", "slack", "whatsapp", "linear"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			src, err := ParseSource(name)
			if err != nil {
				t.Fatalf("ParseSource(%q) error: %v", name, err)
			}
			if string(src) != name {
				t.Errorf("ParseSource(%q) = %q", name, src)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		_, err := ParseSource("gcalendar")
		if err == nil {
			t.Error("expected error for unknown source")
		}
	})
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
		t.Run(string(tt.src), func(t *testing.T) {
			if got := tt.src.Platform(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
