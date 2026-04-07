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

// ListOpts controls which conversations are returned by ListConversations.
// All fields are optional filters; zero values mean "no filter".
type ListOpts struct {
	Platform string        // filter to a single platform
	Account  string        // filter to a single account (requires Platform)
	Since    time.Duration // only conversations with file activity within this window
}

// ConversationInfo describes a conversation discovered by ListConversations.
type ConversationInfo struct {
	Platform     string
	Account      string
	Conversation string
	Dir          string    // absolute path to conversation directory
	LastModified time.Time // mtime of most recent date file
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

	// ListConversations walks the data tree and returns all conversations
	// matching the given filters. Results are sorted by LastModified descending.
	ListConversations(opts ListOpts) ([]ConversationInfo, error)

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
