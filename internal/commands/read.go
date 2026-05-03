package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/timeutil"
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
	switch p.Platform {
	case "gws":
		return fmt.Errorf("read is not supported for gws accounts (data is organized by service, not conversations)\nuse 'pigeon grep' or 'pigeon glob' to search gws data")
	case "linear":
		return fmt.Errorf("read is not supported for linear accounts (data is organized by issue, not conversations)\nuse 'pigeon grep' or 'pigeon glob' to search linear data")
	}

	s := store.NewFSStore(paths.DefaultDataRoot())
	acct := account.New(p.Platform, p.Account)
	aliases := loadAliases(acct)

	matches, err := findConversations(s, acct, p.Contact, aliases)
	if err != nil {
		return err
	}

	opts := store.ReadOpts{
		Date: p.Date,
		Last: p.Last,
	}
	if p.Since != "" {
		d, err := timeutil.ParseDuration(p.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", p.Since, err)
		}
		opts.Since = d
	}
	// `pigeon read` with no filter defaults to the last 25 messages — a
	// terminal-friendly tail. The store layer itself returns the full
	// conversation; this default is a CLI/UX choice that lives here.
	if opts.Date == "" && opts.Since == 0 && opts.Last == 0 {
		const defaultLast = 25
		opts.Last = defaultLast
	}

	printed := 0
	var errs []error
	for _, conv := range matches {
		df, readErr := s.ReadConversation(acct, conv.dirName, opts)
		if df == nil || len(df.Messages) == 0 {
			if readErr != nil {
				errs = append(errs, readErr)
			}
			continue
		}

		lines := modelv1.FormatDateFile(df, time.Local, readErr)

		convDir := paths.DefaultDataRoot().AccountFor(acct).Conversation(conv.dirName)
		header := fmt.Sprintf("--- %s/%s", acct.Display(), conv.displayName)
		if meta, err := s.ReadMeta(acct, conv.dirName); err != nil {
			errs = append(errs, fmt.Errorf("read metadata for %s: %w", conv.dirName, err))
		} else if meta != nil {
			if ids := modelv1.FormatConvMeta(meta); ids != "" {
				header += " " + ids
			}
		}
		header += " ---"
		if printed > 0 {
			fmt.Println()
		}
		fmt.Println(header)
		fmt.Printf("    %s\n", convDir.Path())
		fmt.Println(strings.Join(lines, "\n"))
		printed++
	}

	if printed == 0 {
		if err := errors.Join(errs...); err != nil {
			return err
		}
		fmt.Println("No messages found.")
	}
	return errors.Join(errs...)
}

// conversation holds directory and display info for a matched conversation.
type conversation struct {
	dirName     string
	displayName string
}

// findConversations returns all conversations matching the query by substring on
// directory name, display name, or alias. Case-insensitive.
func findConversations(s store.Store, acct account.Account, query string, aliases map[string][]string) ([]*conversation, error) {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)

	var matches []*conversation
	for _, dirName := range convs {
		displayName := parseDisplayName(dirName)
		if strings.Contains(strings.ToLower(dirName), q) ||
			strings.Contains(strings.ToLower(displayName), q) {
			matches = append(matches, &conversation{dirName: dirName, displayName: displayName})
			continue
		}
		for _, name := range aliases[dirName] {
			if strings.Contains(strings.ToLower(name), q) {
				dn := displayName
				if len(aliases[dirName]) > 0 {
					dn = aliases[dirName][0]
				}
				matches = append(matches, &conversation{dirName: dirName, displayName: dn})
				break
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no conversation matching %q in %s", query, acct.Display())
	}
	return matches, nil
}

// parseDisplayName extracts a display name from a conversation directory name.
// Formats: "+14155559876_Alice" → "Alice", "#engineering" → "#engineering"
func parseDisplayName(dirName string) string {
	if idx := strings.Index(dirName, "_"); idx > 0 {
		return dirName[idx+1:]
	}
	return dirName
}
