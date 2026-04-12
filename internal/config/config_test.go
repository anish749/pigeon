package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestContextConfigAcceptsScalarOrList(t *testing.T) {
	var cfg Config
	data := []byte(`
contexts:
  work:
    gws: work@company.com
    slack: [acme-corp, vendor-ws]
    whatsapp: "+15551234567"
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	work := cfg.Contexts["work"]
	if got, want := len(work.GWS), 1; got != want {
		t.Fatalf("len(work.GWS) = %d, want %d", got, want)
	}
	if got, want := string(work.GWS[0]), "work@company.com"; got != want {
		t.Fatalf("work.GWS[0] = %q, want %q", got, want)
	}
	if got, want := len(work.Slack), 2; got != want {
		t.Fatalf("len(work.Slack) = %d, want %d", got, want)
	}
	if got, want := string(work.WhatsApp[0]), "+15551234567"; got != want {
		t.Fatalf("work.WhatsApp[0] = %q, want %q", got, want)
	}
}
