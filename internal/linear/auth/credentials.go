// Package auth reads the schpet/linear-cli credentials file so pigeon can
// discover the set of authenticated Linear workspaces without shelling out
// to `linear auth list` and parsing its text output.
//
// The file location and format are owned by schpet/linear-cli; see
// https://github.com/schpet/linear-cli/blob/main/src/credentials.ts. Match
// that resolution exactly so pigeon and the linear CLI always agree on
// which file is "the" credentials file.
package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Credentials mirrors the on-disk schema of credentials.toml:
//
//	default = "acme"
//	workspaces = ["acme", "globex"]
//
// API tokens are not stored here — schpet/linear-cli keeps them in the
// system keyring (issue #130). This file is workspace metadata only.
type Credentials struct {
	Default    string   `toml:"default"`
	Workspaces []string `toml:"workspaces"`
}

// ErrNotAuthenticated is returned when no credentials file is found at the
// resolved path. Callers should prompt the user to run `linear auth login`.
var ErrNotAuthenticated = errors.New("linear CLI not authenticated: credentials file not found (run `linear auth login`)")

// Load reads the linear CLI credentials file from the path resolved by
// credentialsPath and returns the parsed contents.
func Load() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	return loadFrom(path)
}

func loadFrom(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotAuthenticated, path)
		}
		return nil, fmt.Errorf("read linear credentials %s: %w", path, err)
	}

	var creds Credentials
	if err := toml.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse linear credentials %s: %w", path, err)
	}
	return &creds, nil
}

// credentialsPath returns the path to the linear CLI credentials file using
// the same resolution as schpet/linear-cli's getCredentialsPath():
//
//   - $XDG_CONFIG_HOME/linear/credentials.toml when XDG_CONFIG_HOME is set
//   - $HOME/.config/linear/credentials.toml otherwise
//
// Returns an error when neither env var is set, matching the upstream
// behaviour of returning null (= unresolvable path).
func credentialsPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "linear", "credentials.toml"), nil
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".config", "linear", "credentials.toml"), nil
	}
	return "", errors.New("cannot resolve linear credentials path: neither XDG_CONFIG_HOME nor HOME is set")
}
