// Package account provides a canonical representation of a messaging account
// (platform + account name pair). Slugs and display names are computed from a
// single source of truth. For filesystem paths, use the paths.DataRoot type chain.
package account

import (
	"strings"

	"github.com/gosimple/slug"
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

// NewFromSlug creates an Account from a platform and an already-slugified
// account name (e.g. from a directory listing). The slug is used as-is for
// both Name and NameSlug since the original display name is not recoverable.
func NewFromSlug(platform, nameSlug string) Account {
	return Account{
		Platform: strings.ToLower(platform),
		Name:     nameSlug,
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

// NameSlug returns just the slugified account name: "coding-with-anish".
func (a Account) NameSlug() string {
	return slug.Make(a.Name)
}

// SlugPath returns the platform/slug form: "slack/coding-with-anish".
// Suitable for config lookups and display-name mappings.
func (a Account) SlugPath() string {
	return a.Platform + "/" + slug.Make(a.Name)
}
