package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStringOrSliceUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want StringOrSlice
	}{
		{"scalar", `"hello"`, StringOrSlice{"hello"}},
		{"array", `["a", "b"]`, StringOrSlice{"a", "b"}},
		{"unquoted scalar", `hello`, StringOrSlice{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got StringOrSlice
			if err := yaml.Unmarshal([]byte(tt.yaml), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestStringOrSliceMarshal(t *testing.T) {
	tests := []struct {
		name string
		val  StringOrSlice
		want string
	}{
		{"single", StringOrSlice{"hello"}, "hello\n"},
		{"multiple", StringOrSlice{"a", "b"}, "- a\n- b\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.val)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.want {
				t.Errorf("got %q, want %q", string(data), tt.want)
			}
		})
	}
}

func TestContextConfigRoundTrip(t *testing.T) {
	input := `contexts:
    work:
        gws: work@company.com
        slack: acme-corp
    personal:
        gws: user@gmail.com
        slack: side-project
        whatsapp: "+15551234567"
    project-x:
        slack:
            - acme-corp
            - vendor-ws
        whatsapp: "+15551234567"
default_context: personal
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.DefaultContext != "personal" {
		t.Errorf("default_context = %q, want %q", cfg.DefaultContext, "personal")
	}
	if len(cfg.Contexts) != 3 {
		t.Fatalf("got %d contexts, want 3", len(cfg.Contexts))
	}

	// work context: gws is single string
	work := cfg.Contexts["work"]
	if got := work.Accounts("gws"); len(got) != 1 || got[0] != "work@company.com" {
		t.Errorf("work.gws = %v, want [work@company.com]", got)
	}
	if got := work.Accounts("slack"); len(got) != 1 || got[0] != "acme-corp" {
		t.Errorf("work.slack = %v, want [acme-corp]", got)
	}

	// personal context: whatsapp is single string
	personal := cfg.Contexts["personal"]
	if got := personal.Accounts("whatsapp"); len(got) != 1 || got[0] != "+15551234567" {
		t.Errorf("personal.whatsapp = %v, want [+15551234567]", got)
	}

	// project-x: slack is array
	px := cfg.Contexts["project-x"]
	if got := px.Accounts("slack"); len(got) != 2 || got[0] != "acme-corp" || got[1] != "vendor-ws" {
		t.Errorf("project-x.slack = %v, want [acme-corp vendor-ws]", got)
	}

	// missing platform returns nil
	if got := work.Accounts("whatsapp"); got != nil {
		t.Errorf("work.whatsapp = %v, want nil", got)
	}
}
