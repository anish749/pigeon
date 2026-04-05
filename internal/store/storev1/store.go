// Package storev1 implements the Pigeon Storage Protocol V1.
//
// The Store type owns a base directory and provides structured read/write
// access to conversation data. All files follow the protocol V1 format
// defined in docs/protocol.md.
package storev1

import (
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// ReadOpts controls which messages are returned by ReadConversation.
type ReadOpts struct {
	Date  string        // specific date "YYYY-MM-DD", empty for default
	Since time.Duration // messages within this duration from now
	Last  int           // last N messages
}

// SearchOpts controls the scope of a search.
type SearchOpts struct {
	Platform string        // filter to platform, empty for all
	Account  string        // filter to account slug, empty for all
	Since    time.Duration // search within this duration from now
}

// SearchResult is a set of matching lines from one conversation/date.
type SearchResult struct {
	Platform     string
	Account      string
	Conversation string
	Date         string
	Lines        []modelv1.Line
	MatchCount   int
}

// Store provides structured read/write access to pigeon's message storage.
type Store interface {
	// Append writes a single event line to the appropriate date file for
	// a conversation. The target file is derived from the line's timestamp.
	Append(acct account.Account, conversation string, line modelv1.Line) error

	// AppendThread writes a single event line to a thread file.
	AppendThread(acct account.Account, conversation, threadTS string, line modelv1.Line) error

	// ReadConversation loads messages from a conversation, applying in-memory
	// compaction (dedup, sort, edit/delete reconciliation, reaction aggregation).
	ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.DateFile, error)

	// ReadThread loads a thread file, applying in-memory compaction.
	ReadThread(acct account.Account, conversation, threadTS string) (*modelv1.ThreadFile, error)

	// Search finds messages matching a query across conversations.
	Search(query string, opts SearchOpts) ([]SearchResult, error)

	// ListPlatforms returns all platform directories (e.g. "slack", "whatsapp").
	ListPlatforms() ([]string, error)

	// ListAccounts returns all account directories for a platform.
	ListAccounts(platform string) ([]string, error)

	// ListConversations returns all conversation directories for an account.
	ListConversations(acct account.Account) ([]string, error)

	// Maintain runs the maintenance pass for an account: dedup, sort,
	// reconcile edits/deletes/reactions, and rewrite modified files.
	Maintain(acct account.Account) error
}
