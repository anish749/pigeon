package paths

import (
	"path/filepath"
	"regexp"
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

const (
	// ConvMetaFilename is the filename for a conversation's metadata sidecar.
	ConvMetaFilename = ".meta.json"

	// FileExt is the file extension for all message data files.
	FileExt = ".jsonl"

	// IdentitySubdir is the subdirectory name for identity files within an account directory.
	IdentitySubdir = "identity"

	// PeopleFilename is the filename for the per-account identity JSONL file.
	PeopleFilename = "people.jsonl"

	// PeopleFileGlob is the rg --glob pattern that matches all people.jsonl
	// identity files under a data root.
	PeopleFileGlob = "**/" + IdentitySubdir + "/" + PeopleFilename

	// SyncCursorsFilename is the filename for an account's per-poller sync cursors.
	SyncCursorsFilename = ".sync-cursors.yaml"

	// MaintenanceFilename is the filename for an account's maintenance state sidecar.
	MaintenanceFilename = ".maintenance.json"

	// WorkspacesSubdir is the top-level directory under the data root that holds
	// per-workspace state (one subdirectory per workspace name).
	WorkspacesSubdir = ".workspaces"

	// WorkstreamSubdir is the per-workspace subdirectory that holds the persistent
	// workstream store: <root>/.workspaces/<name>/workstream/.
	WorkstreamSubdir = "workstream"

	// WorkstreamsFilename is the filename for a workspace's persisted workstream
	// list inside the workstream store directory.
	WorkstreamsFilename = "workstreams.json"

	// WorkstreamProposalsFilename is the filename for a workspace's persisted
	// proposal list inside the workstream store directory.
	WorkstreamProposalsFilename = "proposals.json"
)

// Data directory type hierarchy:
//
//	DataRoot → PlatformDir → AccountDir → ConversationDir
//	                                    ↘ IdentityDir
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
	return AccountDir{platform: r.Platform(acct.Platform), slug: acct.NameSlug()}
}

// Workspace returns a WorkspaceDir for the given workspace name.
func (r DataRoot) Workspace(name string) WorkspaceDir {
	return WorkspaceDir{root: r, name: name}
}

// WorkspaceDir represents a workspace-scoped directory: <base>/.workspaces/<name>/
type WorkspaceDir struct {
	root DataRoot
	name string
}

// Path returns the workspace directory path.
func (w WorkspaceDir) Path() string {
	return filepath.Join(w.root.base, WorkspacesSubdir, w.name)
}

// WorkstreamStore returns the path to the workstream store directory.
func (w WorkspaceDir) WorkstreamStore() WorkstreamStoreDir {
	return WorkstreamStoreDir(filepath.Join(w.Path(), WorkstreamSubdir))
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

// Identity returns the IdentityDir for this account:
//
//	<base>/<platform>/<account-slug>/identity/
func (a AccountDir) Identity() IdentityDir {
	return IdentityDir{account: a}
}

// SyncCursorsFile returns the path to the sync cursors file for this account.
func (a AccountDir) SyncCursorsFile() SyncCursorsFile {
	return SyncCursorsFile(filepath.Join(a.Path(), SyncCursorsFilename))
}

// MaintenanceFile returns the path to the maintenance state file for this account.
func (a AccountDir) MaintenanceFile() MaintenanceFile {
	return MaintenanceFile(filepath.Join(a.Path(), MaintenanceFilename))
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
func (c ConversationDir) MetaFile() ConvMetaFile {
	return ConvMetaFile(filepath.Join(c.Path(), ConvMetaFilename))
}

// DateFile returns the path to a daily message file.
func (c ConversationDir) DateFile(date string) MessagingDateFile {
	return MessagingDateFile(filepath.Join(c.Path(), date+FileExt))
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

// dateFilePattern matches filenames of the form YYYY-MM-DD.jsonl.
var dateFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

// IsDateFile reports whether the given filename matches the YYYY-MM-DD.jsonl
// date file format.
func IsDateFile(name string) bool {
	return dateFilePattern.MatchString(name)
}

// IsThreadFile reports whether the given file path is a thread file.
// A thread file has its parent directory named ThreadsSubdir AND its
// filename is NOT a date file. A conversation literally named "threads"
// has YYYY-MM-DD.jsonl children under its own path, which must not be
// misclassified as thread files.
func IsThreadFile(path string) bool {
	if filepath.Base(filepath.Dir(path)) != ThreadsSubdir {
		return false
	}
	return !IsDateFile(filepath.Base(path))
}

// ThreadsDir returns the path to the threads subdirectory.
func (c ConversationDir) ThreadsDir() string {
	return filepath.Join(c.Path(), ThreadsSubdir)
}

// ThreadFile returns the path to a specific thread file.
func (c ConversationDir) ThreadFile(threadTS string) ThreadFile {
	return ThreadFile(filepath.Join(c.Path(), ThreadsSubdir, threadTS+FileExt))
}

// IsIdentityFile reports whether the given file path lives under the identity subdirectory.
func IsIdentityFile(path string) bool {
	return filepath.Base(filepath.Dir(path)) == IdentitySubdir
}

// PeopleFile is the absolute path to a people.jsonl identity file.
type PeopleFile string

// Path returns the file path as a string.
func (p PeopleFile) Path() string { return string(p) }
func (PeopleFile) dataFile()      {}

// IdentityDir represents the identity directory for an account:
//
//	<base>/<platform>/<account-slug>/identity/
type IdentityDir struct {
	account AccountDir
}

// Path returns the identity directory path.
func (i IdentityDir) Path() string {
	return filepath.Join(i.account.Path(), IdentitySubdir)
}

// PeopleFile returns the path to the people.jsonl file for this account.
func (i IdentityDir) PeopleFile() PeopleFile {
	return PeopleFile(filepath.Join(i.Path(), PeopleFilename))
}
