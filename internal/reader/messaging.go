package reader

import (
	"fmt"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
)

// MessagingResult holds the output of reading a messaging conversation
// (Slack or WhatsApp).
type MessagingResult struct {
	Account      account.Account
	Conversation string // directory name
	DisplayName  string // human-friendly name
	Messages     *modelv1.ResolvedDateFile
	Meta         *modelv1.ConvMeta
}

// ReadMessaging reads a messaging conversation (Slack or WhatsApp) using
// the existing store layer. The selector is fuzzy-matched against
// conversation directory names.
func ReadMessaging(s store.Store, acct account.Account, selector string, filters Filters) (*MessagingResult, error) {
	conv, err := findConversation(s, acct, selector)
	if err != nil {
		return nil, err
	}

	opts := store.ReadOpts{
		Date: filters.Date,
		Last: filters.Last,
	}
	if filters.Since > 0 {
		opts.Since = filters.Since
	}
	if opts.Date == "" && opts.Since == 0 && opts.Last == 0 {
		// Default: today's messages.
		opts.Since = time.Duration(time.Now().Hour())*time.Hour +
			time.Duration(time.Now().Minute())*time.Minute +
			time.Duration(time.Now().Second())*time.Second
		if opts.Since < time.Hour {
			opts.Since = 24 * time.Hour // at least today
		}
	}

	resolved, err := s.ReadConversation(acct, conv.dirName, opts)
	if err != nil {
		return nil, err
	}

	meta, _ := s.ReadMeta(acct, conv.dirName)

	return &MessagingResult{
		Account:      acct,
		Conversation: conv.dirName,
		DisplayName:  conv.displayName,
		Messages:     resolved,
		Meta:         meta,
	}, nil
}

// conversation holds directory and display info for a matched conversation.
type conversation struct {
	dirName     string
	displayName string
}

// findConversation fuzzy-matches a selector against conversation directories.
func findConversation(s store.Store, acct account.Account, selector string) (*conversation, error) {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(selector)
	var matches []conversation
	for _, dirName := range convs {
		displayName := parseDisplayName(dirName)
		if strings.Contains(strings.ToLower(dirName), q) ||
			strings.Contains(strings.ToLower(displayName), q) {
			matches = append(matches, conversation{dirName: dirName, displayName: displayName})
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no conversation matching %q in %s", selector, acct.Display())
	case 1:
		return &matches[0], nil
	default:
		// Check for exact match first.
		for _, m := range matches {
			if strings.EqualFold(m.dirName, selector) || strings.EqualFold(m.displayName, selector) {
				return &m, nil
			}
		}
		var names []string
		for _, m := range matches {
			names = append(names, m.displayName)
		}
		return nil, fmt.Errorf("ambiguous conversation %q in %s — matches: %s",
			selector, acct.Display(), strings.Join(names, ", "))
	}
}

// parseDisplayName extracts a display name from a conversation directory name.
// Formats: "+14155559876_Alice" → "Alice", "#engineering" → "#engineering"
func parseDisplayName(dirName string) string {
	if idx := strings.Index(dirName, "_"); idx > 0 {
		return dirName[idx+1:]
	}
	return dirName
}
