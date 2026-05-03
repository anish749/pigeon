package auth

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCredentialsPath(t *testing.T) {
	tests := []struct {
		name   string
		xdg    string
		home   string
		want   string
		errMsg string
	}{
		{
			name: "xdg set wins over home",
			xdg:  "/tmp/xdg",
			home: "/tmp/home",
			want: "/tmp/xdg/linear/credentials.toml",
		},
		{
			name: "xdg empty falls back to home",
			xdg:  "",
			home: "/tmp/home",
			want: "/tmp/home/.config/linear/credentials.toml",
		},
		{
			name:   "both empty errors",
			xdg:    "",
			home:   "",
			errMsg: "cannot resolve linear credentials path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)

			got, err := credentialsPath()
			if tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("want error containing %q, got %v", tt.errMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadFrom(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content string
		want    *Credentials
		errIs   error
		errMsg  string
	}{
		{
			name:    "happy path two workspaces",
			content: "default = \"acme\"\nworkspaces = [\"acme\", \"globex\"]\n",
			want:    &Credentials{Default: "acme", Workspaces: []string{"acme", "globex"}},
		},
		{
			name:    "single workspace",
			content: "default = \"acme\"\nworkspaces = [\"acme\"]\n",
			want:    &Credentials{Default: "acme", Workspaces: []string{"acme"}},
		},
		{
			name:    "empty workspaces array",
			content: "default = \"\"\nworkspaces = []\n",
			want:    &Credentials{Default: "", Workspaces: []string{}},
		},
		{
			name:    "malformed TOML",
			content: "default = acme\n", // unquoted value
			errMsg:  "parse linear credentials",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".toml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := loadFrom(path)
			if tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("want error containing %q, got %v", tt.errMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadFromMissingFile(t *testing.T) {
	_, err := loadFrom(filepath.Join(t.TempDir(), "nope.toml"))
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("want ErrNotAuthenticated, got %v", err)
	}
}
