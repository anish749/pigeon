// Package storev1 implements the Pigeon Storage Protocol V1.
//
// The Store type owns a base directory and provides structured read/write
// access to conversation data. All files follow the protocol V1 format
// defined in docs/protocol.md.
package store

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

// Store provides structured read/write access to pigeon's message storage.
type Store interface {
	// Append writes a single event line to the appropriate date file for
	// a conversation. The target file is derived from the line's timestamp.
	Append(acct account.Account, conversation string, line modelv1.Line) error

	// AppendThread writes a single event line to a thread file.
	AppendThread(acct account.Account, conversation, threadTS string, line modelv1.Line) error

	// ReadConversation loads messages from a conversation, applying compaction
	// and resolution (reactions grouped onto messages).
	ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.ResolvedDateFile, error)

	// ReadThread loads a thread file, applying compaction and resolution.
	ReadThread(acct account.Account, conversation, threadTS string) (*modelv1.ResolvedThreadFile, error)

	// ThreadExists checks if a thread file exists for the given thread timestamp.
	ThreadExists(acct account.Account, conversation, threadTS string) bool

	// MessageExists checks if a message with the given ID exists in any
	// date file for the conversation.
	MessageExists(acct account.Account, conversation, messageID string) bool

	// ListPlatforms returns all platform directories (e.g. "slack", "whatsapp").
	ListPlatforms() ([]string, error)

	// ListAccounts returns all account directories for a platform.
	ListAccounts(platform string) ([]string, error)

	// ListConversations returns all conversation directories for an account.
	ListConversations(acct account.Account) ([]string, error)

	// WriteMetaIfNotExists writes .meta.json only if it doesn't already exist.
	// Returns true if written, false if already present.
	WriteMetaIfNotExists(acct account.Account, conversation string, meta modelv1.ConvMeta) (bool, error)

	// ReadMeta reads the .meta.json sidecar for a conversation.
	// Returns nil, nil if the file does not exist.
	ReadMeta(acct account.Account, conversation string) (*modelv1.ConvMeta, error)

	// Maintain runs the maintenance pass for an account: dedup, sort,
	// reconcile edits/deletes/reactions, and rewrite modified files.
	Maintain(acct account.Account) error
}
