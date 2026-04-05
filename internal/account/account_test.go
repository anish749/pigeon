package account

import (
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		platform     string
		name         string
		wantPlatform string
		wantName     string
	}{
		{"slack", "My Workspace", "slack", "My Workspace"},
		{"Slack", "My Workspace", "slack", "My Workspace"},
		{"WHATSAPP", "+1234567890", "whatsapp", "+1234567890"},
		{"slack", "", "slack", ""},
	}
	for _, tt := range tests {
		a := New(tt.platform, tt.name)
		if a.Platform != tt.wantPlatform {
			t.Errorf("New(%q, %q).Platform = %q, want %q", tt.platform, tt.name, a.Platform, tt.wantPlatform)
		}
		if a.Name != tt.wantName {
			t.Errorf("New(%q, %q).Name = %q, want %q", tt.platform, tt.name, a.Name, tt.wantName)
		}
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		platform string
		name     string
		want     string
	}{
		{"slack", "My Workspace", "slack-my-workspace"},
		{"whatsapp", "+1234567890", "whatsapp-1234567890"},
		{"slack", "Coding With Anish", "slack-coding-with-anish"},
		{"Slack", "ALL CAPS NAME", "slack-all-caps-name"},
		{"slack", "already-slugged", "slack-already-slugged"},
	}
	for _, tt := range tests {
		a := New(tt.platform, tt.name)
		if got := a.String(); got != tt.want {
			t.Errorf("New(%q, %q).String() = %q, want %q", tt.platform, tt.name, got, tt.want)
		}
	}
}

func TestDisplay(t *testing.T) {
	tests := []struct {
		platform string
		name     string
		want     string
	}{
		{"slack", "My Workspace", "slack/My Workspace"},
		{"whatsapp", "+1234567890", "whatsapp/+1234567890"},
		{"Slack", "Coding With Anish", "slack/Coding With Anish"},
	}
	for _, tt := range tests {
		a := New(tt.platform, tt.name)
		if got := a.Display(); got != tt.want {
			t.Errorf("New(%q, %q).Display() = %q, want %q", tt.platform, tt.name, got, tt.want)
		}
	}
}

func TestNameSlug(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"My Workspace", "my-workspace"},
		{"+1234567890", "1234567890"},
		{"Coding With Anish", "coding-with-anish"},
		{"already-slugged", "already-slugged"},
	}
	for _, tt := range tests {
		a := New("slack", tt.name)
		if got := a.NameSlug(); got != tt.want {
			t.Errorf("New(\"slack\", %q).NameSlug() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSlugIdempotent(t *testing.T) {
	// Slugifying an already-slugified name should be a no-op.
	a := New("slack", "coding-with-anish")
	if got := a.NameSlug(); got != "coding-with-anish" {
		t.Errorf("NameSlug() of already-slugged name = %q, want %q", got, "coding-with-anish")
	}
}

func TestStringUsableAsMapKey(t *testing.T) {
	// Two accounts constructed from different casing should produce the same key.
	a := New("Slack", "My Workspace")
	b := New("slack", "My Workspace")
	if a.String() != b.String() {
		t.Errorf("String() not stable across platform casing: %q vs %q", a.String(), b.String())
	}

	// Different names should produce different keys.
	c := New("slack", "Other Workspace")
	if a.String() == c.String() {
		t.Errorf("different accounts produced same String(): %q", a.String())
	}
}
