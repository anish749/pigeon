package commands

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/utils/timeutil"
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

// runReadJira streams the issue snapshot log + comments log for one Jira
// issue, parsing each line through modelv1 and re-marshalling it on the
// way out. The round-trip validates structure (parse failures surface
// with line-number context) without altering the on-disk shape — agent
// callers consume the platform's native JSON unchanged.
//
// --date / --last / --since are rejected: each issue is one logical unit,
// not a date-windowed conversation. Use `pigeon grep` for cross-issue
// search.
//
// Missing comments.jsonl is tolerated (issues with zero comments leave the
// file absent until the first comment lands). Missing issue.jsonl is an
// error — FindJiraIssue would not return it without that file.
func runReadJira(p ReadParams) error {
	if p.Date != "" || p.Last != 0 || p.Since != "" {
		return fmt.Errorf("read for jira does not support --date, --last, or --since (each issue is one file; use 'pigeon grep' to search across issues)")
	}

	acct := account.New(p.Platform, p.Account)
	jd := paths.DefaultDataRoot().AccountFor(acct).Jira()

	issueFile, err := read.FindJiraIssue(jd, p.Contact)
	if err != nil {
		return err
	}
	if err := streamModelLines(issueFile.Path(), false); err != nil {
		return err
	}
	if err := streamModelLines(issueFile.CommentsFile().Path(), true); err != nil {
		return err
	}
	return nil
}

// streamModelLines reads a JSONL file line by line, parses each line via
// modelv1.Parse, and writes the canonical re-serialization to stdout.
// Round-trips validate structure on read (parse failures surface with
// line-number context) without changing the on-disk shape.
//
// When tolerateMissing is true, a non-existent file is treated as empty
// (used for comments.jsonl, which only exists once a comment lands).
func streamModelLines(path string, tolerateMissing bool) error {
	f, err := os.Open(path)
	if err != nil {
		if tolerateMissing && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	lineNum := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				l, err := modelv1.Parse(string(trimmed))
				if err != nil {
					return fmt.Errorf("parse line %d in %s: %w", lineNum, path, err)
				}
				marshalled, err := modelv1.Marshal(l)
				if err != nil {
					return fmt.Errorf("marshal line %d in %s: %w", lineNum, path, err)
				}
				if _, err := out.Write(marshalled); err != nil {
					return fmt.Errorf("write stdout: %w", err)
				}
				if err := out.WriteByte('\n'); err != nil {
					return fmt.Errorf("write stdout: %w", err)
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read %s: %w", path, readErr)
		}
	}
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
