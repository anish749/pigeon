package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/storev1"
)

type ReadParams struct {
	Platform string
	Account  string
	Contact  string
	Date     string
	Last     int
	Since    string
}

func RunRead(p ReadParams) error {
	s := storev1.NewFSStore(paths.DataDir())
	acct := account.New(p.Platform, p.Account)
	aliases := loadAliases(acct)

	conv, err := findConversation(s, acct, p.Contact, aliases)
	if err != nil {
		return err
	}

	opts := storev1.ReadOpts{
		Date: p.Date,
		Last: p.Last,
	}
	if p.Since != "" {
		d, err := parseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		opts.Since = d
	}

	df, err := s.ReadConversation(acct, conv.dirName, opts)
	if err != nil {
		return err
	}

	if df == nil || len(df.Messages) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	lines := modelv1.FormatDateFile(df, time.Local, true)

	dir := acct.ConversationDir(conv.dirName)
	fmt.Printf("--- %s/%s ---\n", acct.Display(), conv.displayName)
	fmt.Printf("    %s\n", dir)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// parseDuration handles Go durations plus "d" for days.
func parseDuration(s string) (time.Duration, error) {
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		var days int
		if _, err := fmt.Sscanf(rest, "%d", &days); err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// conversation holds directory and display info for a matched conversation.
type conversation struct {
	dirName     string
	displayName string
}

// findConversation searches for a conversation matching the query by directory name,
// display name, or alias. Returns the first match. Case-insensitive.
func findConversation(s storev1.Store, acct account.Account, query string, aliases map[string][]string) (*conversation, error) {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	for _, dirName := range convs {
		displayName := parseDisplayName(dirName)
		if strings.Contains(strings.ToLower(dirName), q) ||
			strings.Contains(strings.ToLower(displayName), q) {
			return &conversation{dirName: dirName, displayName: displayName}, nil
		}
		for _, name := range aliases[dirName] {
			if strings.Contains(strings.ToLower(name), q) {
				dn := displayName
				if len(aliases[dirName]) > 0 {
					dn = aliases[dirName][0]
				}
				return &conversation{dirName: dirName, displayName: dn}, nil
			}
		}
	}
	return nil, fmt.Errorf("no conversation matching %q in %s", query, acct.Display())
}

// parseDisplayName extracts a display name from a conversation directory name.
// Formats: "+14155559876_Alice" → "Alice", "#engineering" → "#engineering"
func parseDisplayName(dirName string) string {
	if idx := strings.Index(dirName, "_"); idx > 0 {
		return dirName[idx+1:]
	}
	return dirName
}

