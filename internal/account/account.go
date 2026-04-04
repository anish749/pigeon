// Package account provides a canonical representation of a messaging account
// (platform + account name pair). All name-derived forms (slugs, display names,
// data directory paths) are computed from a single source of truth.
package account

import (
	"path/filepath"
	"strings"

	"github.com/gosimple/slug"

	"github.com/anish/claude-msg-utils/internal/paths"
)

// Account identifies a messaging account with a platform and display name.
// Construct via New to ensure the Platform field is always lowercase.
type Account struct {
	Platform string // always lowercase: "slack", "whatsapp"
	Name     string // original display name: "Coding With Anish", "+1234567890"
}

// New creates an Account, normalizing the platform to lowercase.
func New(platform, name string) Account {
	return Account{
		Platform: strings.ToLower(platform),
		Name:     name,
	}
}

// String returns the canonical slug form: "slack-coding-with-anish".
// Suitable for map keys, session filenames, and log identifiers.
func (a Account) String() string {
	return a.Platform + "-" + slug.Make(a.Name)
}

// Display returns the human-readable form: "slack/Coding With Anish".
func (a Account) Display() string {
	return a.Platform + "/" + a.Name
}

// DataDir returns the full absolute data directory path for this account,
// e.g. "~/.local/share/pigeon/slack/coding-with-anish".
func (a Account) DataDir() string {
	return filepath.Join(paths.DataDir(), a.Platform, slug.Make(a.Name))
}

// NameSlug returns just the slugified account name: "coding-with-anish".
// Use when platform and account are needed as separate path components.
func (a Account) NameSlug() string {
	return slug.Make(a.Name)
}

// ConversationDir returns the full absolute data directory path for a
// conversation within this account.
func (a Account) ConversationDir(conversation string) string {
	return filepath.Join(a.DataDir(), conversation)
}
