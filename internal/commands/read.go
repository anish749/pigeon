package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/read"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
)

// DefaultGmailLast is the default number of emails when no filter is specified.
const DefaultGmailLast = 25

// ReadGWSLines reads JSONL date files from a GWS service directory (gmail
// or calendar), deduplicates by ID, excludes cancelled calendar events,
// applies time and quantity filters, and returns sorted lines.
//
// For --date: constructs the path directly (no directory listing).
// For --since: uses read.Glob to discover files in the time window.
// For no filter / --last: uses read.Glob with since=0 for all files.
func ReadGWSLines(s *store.FSStore, dir string, since time.Duration, date string, last int, defaultLast int) ([]modelv1.Line, error) {
	files, err := globDateFiles(dir, since, date)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Read and parse all files.
	var allLines []modelv1.Line
	var errs []error
	for _, f := range files {
		lines, err := s.ReadLines(paths.DateFile(f))
		if err != nil {
			errs = append(errs, err)
		}
		allLines = append(allLines, lines...)
	}

	// Dedup by ID, keep last occurrence.
	allLines = compact.DedupGWS(allLines)

	// Exclude cancelled calendar events.
	allLines = filterCancelled(allLines)

	// Apply --since precise cutoff (file selection is coarse by date).
	if since > 0 {
		cutoff := time.Now().Add(-since)
		var filtered []modelv1.Line
		for _, l := range allLines {
			ts := l.Ts()
			if ts.IsZero() || !ts.Before(cutoff) {
				filtered = append(filtered, l)
			}
		}
		allLines = filtered
	}

	// Sort by timestamp.
	sort.SliceStable(allLines, func(i, j int) bool {
		return allLines[i].Ts().Before(allLines[j].Ts())
	})

	// Apply --last=N (or default).
	n := last
	if n == 0 && since == 0 && date == "" {
		n = defaultLast
	}
	if n > 0 && len(allLines) > n {
		allLines = allLines[len(allLines)-n:]
	}

	if len(errs) > 0 {
		return allLines, fmt.Errorf("partial read errors: %w", joinErrors(errs))
	}
	return allLines, nil
}

// ReadDriveContent finds a drive file directory by fuzzy match and returns
// its content (markdown/CSV text) and comment lines.
func ReadDriveContent(s *store.FSStore, driveDir paths.DriveDir, selector string) (content string, comments []modelv1.Line, err error) {
	fileDir, err := FuzzyMatchDriveFile(driveDir, selector)
	if err != nil {
		return "", nil, err
	}

	// Read content files (markdown or CSV).
	contentEntries, err := os.ReadDir(fileDir.Path())
	if err != nil {
		return "", nil, fmt.Errorf("read drive file dir: %w", err)
	}
	var buf strings.Builder
	for _, e := range contentEntries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == paths.MarkdownExt || ext == paths.CSVExt {
			data, err := os.ReadFile(filepath.Join(fileDir.Path(), e.Name()))
			if err != nil {
				return "", nil, fmt.Errorf("read %s: %w", e.Name(), err)
			}
			buf.Write(data)
		}
	}

	// Read and dedup comments.
	commentLines, err := s.ReadLines(fileDir.CommentsFile())
	if err != nil {
		return "", nil, fmt.Errorf("read comments: %w", err)
	}
	commentLines = compact.DedupGWS(commentLines)

	return buf.String(), commentLines, nil
}

// ReadMessaging reads a messaging conversation (Slack or WhatsApp) using
// the existing store layer. Returns the resolved date file and the matched
// conversation directory name.
func ReadMessaging(s *store.FSStore, acct account.Account, selector string, since time.Duration, date string, last int) (*modelv1.ResolvedDateFile, string, error) {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return nil, "", err
	}

	conv, err := FuzzyMatchConversation(convs, selector)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", acct.Display(), err)
	}

	opts := store.ReadOpts{Date: date, Last: last, Since: since}
	df, err := s.ReadConversation(acct, conv, opts)
	return df, conv, err
}

// ReadLinearIssue reads a single Linear issue file by exact or fuzzy
// identifier match, deduplicates, and returns the lines.
func ReadLinearIssue(s *store.FSStore, issuesDir, selector string) ([]modelv1.Line, error) {
	matched, err := FuzzyMatchLinearIssue(issuesDir, selector)
	if err != nil {
		return nil, err
	}

	lines, err := s.ReadLines(paths.DateFile(filepath.Join(issuesDir, matched)))
	if err != nil {
		return nil, fmt.Errorf("read issue %s: %w", matched, err)
	}
	return compact.DedupGWS(lines), nil
}

// --- Fuzzy matchers ---

// FuzzyMatchDriveFile finds a drive file directory by substring match on
// directory names. Returns an error with candidates if ambiguous.
func FuzzyMatchDriveFile(driveDir paths.DriveDir, selector string) (paths.DriveFileDir, error) {
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return paths.DriveFileDir{}, fmt.Errorf("no drive data for this account")
		}
		return paths.DriveFileDir{}, fmt.Errorf("read drive dir: %w", err)
	}

	q := strings.ToLower(selector)
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.Contains(strings.ToLower(e.Name()), q) {
			matches = append(matches, e.Name())
		}
	}

	switch len(matches) {
	case 0:
		return paths.DriveFileDir{}, fmt.Errorf("no drive document matching %q", selector)
	case 1:
		return driveDir.File(matches[0]), nil
	default:
		return paths.DriveFileDir{}, fmt.Errorf("ambiguous drive document %q — matches: %s", selector, strings.Join(matches, ", "))
	}
}

// FuzzyMatchConversation finds a conversation directory by substring match.
// Returns an exact match if one exists among multiple substring matches.
func FuzzyMatchConversation(convs []string, query string) (string, error) {
	q := strings.ToLower(query)
	var matches []string
	for _, c := range convs {
		if strings.Contains(strings.ToLower(c), q) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no conversation matching %q", query)
	case 1:
		return matches[0], nil
	default:
		for _, m := range matches {
			if strings.EqualFold(m, query) {
				return m, nil
			}
		}
		return "", fmt.Errorf("ambiguous conversation %q — matches: %s", query, strings.Join(matches, ", "))
	}
}

// FuzzyMatchLinearIssue finds a Linear issue file by exact or fuzzy match
// on the identifier (filename without extension). Returns the full filename.
func FuzzyMatchLinearIssue(issuesDir, selector string) (string, error) {
	entries, err := os.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no linear issue data")
		}
		return "", fmt.Errorf("read issues dir: %w", err)
	}

	q := strings.ToLower(selector)
	var matched string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != paths.FileExt {
			continue
		}
		name := strings.TrimSuffix(e.Name(), paths.FileExt)
		if strings.EqualFold(name, selector) {
			return e.Name(), nil
		}
		if strings.Contains(strings.ToLower(name), q) {
			if matched != "" {
				return "", fmt.Errorf("ambiguous linear issue %q", selector)
			}
			matched = e.Name()
		}
	}
	if matched == "" {
		return "", fmt.Errorf("no linear issue matching %q", selector)
	}
	return matched, nil
}

// --- helpers ---

// globDateFiles discovers date files for reading. For a specific date it
// constructs the path directly. For --since or all-files it delegates to
// read.Glob.
func globDateFiles(dir string, since time.Duration, date string) ([]string, error) {
	switch {
	case date != "":
		p := filepath.Join(dir, date+paths.FileExt)
		if _, err := os.Stat(p); err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
		return []string{p}, nil
	default:
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("stat %s: %w", dir, err)
		}
		files, err := read.Glob(dir, since)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", dir, err)
		}
		return files, nil
	}
}

// filterCancelled removes cancelled calendar events. No-op for non-event lines.
func filterCancelled(lines []modelv1.Line) []modelv1.Line {
	var out []modelv1.Line
	for _, l := range lines {
		if l.Type == modelv1.LineEvent && l.Event != nil && l.Event.Runtime.Status == "cancelled" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	var msgs []string
	for _, e := range errs {
		if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}
