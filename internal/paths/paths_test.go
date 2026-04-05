package paths

import (
	"path/filepath"
	"testing"
)

func TestPlatformDir(t *testing.T) {
	got := PlatformDir("slack")
	want := filepath.Join(DataDir(), "slack")
	if got != want {
		t.Errorf("PlatformDir(slack) = %q, want %q", got, want)
	}
}

func TestAccountDir(t *testing.T) {
	got := AccountDir("slack", "acme-corp")
	want := filepath.Join(DataDir(), "slack", "acme-corp")
	if got != want {
		t.Errorf("AccountDir(slack, acme-corp) = %q, want %q", got, want)
	}
}

func TestPlatformDir_UsesDataDir(t *testing.T) {
	t.Setenv("PIGEON_DATA_DIR", "/tmp/pigeon-test")
	got := PlatformDir("whatsapp")
	if got != "/tmp/pigeon-test/whatsapp" {
		t.Errorf("PlatformDir with env = %q, want /tmp/pigeon-test/whatsapp", got)
	}
}

func TestAccountDir_UsesDataDir(t *testing.T) {
	t.Setenv("PIGEON_DATA_DIR", "/tmp/pigeon-test")
	got := AccountDir("whatsapp", "15551234567")
	if got != "/tmp/pigeon-test/whatsapp/15551234567" {
		t.Errorf("AccountDir with env = %q, want /tmp/pigeon-test/whatsapp/15551234567", got)
	}
}
