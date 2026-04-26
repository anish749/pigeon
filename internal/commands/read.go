package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
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
	case paths.JiraPlatform:
		return runReadJira(p)
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

// runReadJira streams an issue's JSONL file to stdout unchanged. Jira data
// is organized as one issue file per Jira issue (one jira-issue line + N
// jira-comment lines), and the agent consuming this output understands the
// platform's native JSON shape directly — no resolution, formatting, or
// per-line rendering is applied. The --date, --last and --since filters do
// not apply to issue files; specifying any of them is a usage error rather
// than a silent no-op.
//
// Contact resolves to the Jira issue key (e.g. ENG-101). Project subdir is
// not derivable from the key alone (project keys can be opaque, multi-segment
// strings), so the file is located by a single rg --files glob across the
// account directory — the same discovery primitive `pigeon glob` uses.
func runReadJira(p ReadParams) error {
	if p.Date != "" || p.Last != 0 || p.Since != "" {
		return fmt.Errorf("read for jira-issues does not support --date, --last, or --since (each issue is one file; use 'pigeon grep' to search across issues)")
	}

	acct := account.New(p.Platform, p.Account)
	jd := paths.DefaultDataRoot().AccountFor(acct).Jira()

	matches, err := read.GlobFiles(jd.Path(), []string{p.Contact + paths.FileExt})
	if err != nil {
		return fmt.Errorf("locate jira issue %s: %w", p.Contact, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("jira issue %s not found in %s", p.Contact, acct.Display())
	}
	if len(matches) > 1 {
		return fmt.Errorf("ambiguous jira issue %s in %s: %d files match", p.Contact, acct.Display(), len(matches))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", matches[0], err)
	}
	if len(data) == 0 {
		return fmt.Errorf("jira issue %s in %s is empty", p.Contact, acct.Display())
	}
	if _, err := os.Stdout.Write(data); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
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
