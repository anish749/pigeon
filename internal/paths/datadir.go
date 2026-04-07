package paths

import (
	"path/filepath"
	"strings"

	"github.com/anish749/pigeon/internal/account"
)

// SearchDir returns the data directory scoped by optional platform and account
// filters. With no filters, returns the data root. With platform, returns the
// platform directory. With both, returns the account directory.
func SearchDir(platform, accountName string) string {
	root := DefaultDataRoot()
	switch {
	case platform != "" && accountName != "":
		return root.AccountFor(account.New(platform, accountName)).Path()
	case platform != "":
		return root.Platform(platform).Path()
	default:
		return root.Path()
	}
}

// Data directory type hierarchy:
//
//	DataRoot → PlatformDir → AccountDir → ConversationDir
//
// Each level carries accumulated path segments and exposes Path() string.
// Slugification lives in the account package; paths only accepts slugs or Account objects.
// The base directory is injected via NewDataRoot, so FSStore can use a test
// directory while commands use DefaultDataRoot().

// DataRoot is the root of the message data tree.
type DataRoot struct{ base string }

// NewDataRoot creates a DataRoot with a custom base directory.
func NewDataRoot(base string) DataRoot { return DataRoot{base: base} }

// DefaultDataRoot returns a DataRoot rooted at the default data directory.
func DefaultDataRoot() DataRoot { return DataRoot{base: DataDir()} }

// Path returns the root data directory.
func (r DataRoot) Path() string { return r.base }

// Platform returns a PlatformDir for the given platform.
func (r DataRoot) Platform(platform string) PlatformDir {
	return PlatformDir{root: r, platform: strings.ToLower(platform)}
}

// AccountFor returns an AccountDir from an account.Account.
func (r DataRoot) AccountFor(acct account.Account) AccountDir {
	return r.Platform(acct.Platform).AccountFromSlug(acct.NameSlug())
}

// PlatformDir represents a platform directory: <base>/<platform>/
type PlatformDir struct {
	root     DataRoot
	platform string
}

// Path returns the platform directory path.
func (p PlatformDir) Path() string {
	return filepath.Join(p.root.base, p.platform)
}

// AccountFromSlug returns an AccountDir from an already-slugified name
// (e.g. from a directory listing).
func (p PlatformDir) AccountFromSlug(s string) AccountDir {
	return AccountDir{platform: p, slug: s}
}

// AccountDir represents an account directory: <base>/<platform>/<account-slug>/
type AccountDir struct {
	platform PlatformDir
	slug     string
}

// Path returns the account directory path.
func (a AccountDir) Path() string {
	return filepath.Join(a.platform.Path(), a.slug)
}

// Conversation returns a ConversationDir for the given conversation name.
func (a AccountDir) Conversation(name string) ConversationDir {
	return ConversationDir{account: a, name: name}
}

// SyncCursorsPath returns the path to the sync cursors file for this account.
func (a AccountDir) SyncCursorsPath() string {
	return filepath.Join(a.Path(), ".sync-cursors.yaml")
}

// MaintenancePath returns the path to the maintenance state file for this account.
func (a AccountDir) MaintenancePath() string {
	return filepath.Join(a.Path(), ".maintenance.json")
}

// ConversationDir represents a conversation directory: <base>/<platform>/<account-slug>/<conversation>/
type ConversationDir struct {
	account AccountDir
	name    string
}

// Path returns the conversation directory path.
func (c ConversationDir) Path() string {
	return filepath.Join(c.account.Path(), c.name)
}

// MetaFile returns the path to the conversation's .meta.json sidecar.
func (c ConversationDir) MetaFile() string {
	return filepath.Join(c.Path(), ".meta.json")
}

// FileExt is the file extension for all message data files.
const FileExt = ".jsonl"

// DateFile returns the path to a daily message file.
func (c ConversationDir) DateFile(date string) string {
	return filepath.Join(c.Path(), date+FileExt)
}

// Thread directory and file glob patterns for search tools.
const (
	// ThreadsSubdir is the directory name for thread files within a conversation.
	ThreadsSubdir = "threads"

	// ThreadGlobRg is the glob pattern for rg --glob to match thread files
	// nested at <conversation>/threads/<ts>.jsonl.
	ThreadGlobRg = "**/" + ThreadsSubdir + "/*" + FileExt

	// ThreadGlobFind is the -path pattern for find(1) to match thread files.
	ThreadGlobFind = "*/" + ThreadsSubdir + "/*" + FileExt
)

// ThreadsDir returns the path to the threads subdirectory.
func (c ConversationDir) ThreadsDir() string {
	return filepath.Join(c.Path(), ThreadsSubdir)
}

// ThreadFile returns the path to a specific thread file.
func (c ConversationDir) ThreadFile(threadTS string) string {
	return filepath.Join(c.Path(), ThreadsSubdir, threadTS+FileExt)
}
