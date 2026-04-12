package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/config"
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/pctx"
	"github.com/anish749/pigeon/internal/store"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
	"github.com/anish749/pigeon/internal/timeutil"
)

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <source> [selector]",
		Short: "Read data from a source",
		Long: `Read data from a source within the active context.

Sources: gmail, calendar, drive, slack, whatsapp, linear

The active context determines which accounts are queried. Override with
--context or -a. When no context is set and only one account exists for
the source's platform, it is used automatically.`,
		GroupID: groupReading,
		Example: `  pigeon read gmail --since=7d
  pigeon read calendar
  pigeon read drive "Q2 Planning"
  pigeon read slack #engineering --since=2h
  pigeon read whatsapp Alice --last=20
  pigeon read linear TRU-253
  pigeon read gmail --context=work --since=24h
  pigeon read calendar -a work@company.com`,
		Args:    cobra.RangeArgs(1, 2),
		PreRunE: ensureDaemon,
		RunE:    runRead,
	}
	cmd.Flags().String("context", "", "override active context")
	cmd.Flags().StringP("account", "a", "", "bypass context, use specific account")
	cmd.Flags().String("date", "", "specific date (YYYY-MM-DD)")
	cmd.Flags().Int("last", 0, "last N items")
	cmd.Flags().String("since", "", "items within duration (e.g. 30m, 2h, 7d)")
	cmd.Flags().String("calendar", "", "calendar ID (default: primary)")
	return cmd
}

func runRead(cmd *cobra.Command, args []string) error {
	src, err := pctx.ParseSource(args[0])
	if err != nil {
		return err
	}

	selector := ""
	if len(args) > 1 {
		selector = args[1]
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	contextFlag, err := cmd.Flags().GetString("context")
	if err != nil {
		return fmt.Errorf("get context flag: %w", err)
	}
	accountFlag, err := cmd.Flags().GetString("account")
	if err != nil {
		return fmt.Errorf("get account flag: %w", err)
	}

	ctxName := pctx.ResolveContextName(contextFlag, os.Getenv("PIGEON_CONTEXT"), cfg)
	resolved, err := pctx.Resolve(cfg, src, pctx.ResolveOpts{
		Context: ctxName,
		Account: accountFlag,
	})
	if err != nil {
		return err
	}

	root := paths.DefaultDataRoot()
	s := store.NewFSStore(root)
	acct := resolved.Accounts[0]

	since, date, last, err := parseTimeFilters(cmd)
	if err != nil {
		return err
	}

	switch src {
	case pctx.SourceGmail:
		return readGWSDateFiles(s, root.AccountFor(acct).Gmail().Path(), since, date, last, DefaultGmailLast)

	case pctx.SourceCalendar:
		calID, err := cmd.Flags().GetString("calendar")
		if err != nil {
			return fmt.Errorf("get calendar flag: %w", err)
		}
		if calID == "" {
			calID = "primary"
		}
		// Calendar defaults to today when no filter specified.
		if since == 0 && date == "" && last == 0 {
			date = time.Now().Format("2006-01-02")
		}
		return readGWSDateFiles(s, root.AccountFor(acct).Calendar(calID).Path(), since, date, last, 0)

	case pctx.SourceDrive:
		if selector == "" {
			return fmt.Errorf("pigeon read drive requires a document name")
		}
		return readDrive(s, root.AccountFor(acct).Drive(), selector)

	case pctx.SourceSlack:
		if selector == "" {
			return fmt.Errorf("pigeon read slack requires a channel or DM (e.g. #engineering, @alice)")
		}
		return readMessaging(s, acct, selector, since, date, last)

	case pctx.SourceWhatsApp:
		if selector == "" {
			return fmt.Errorf("pigeon read whatsapp requires a contact or group name")
		}
		return readMessaging(s, acct, selector, since, date, last)

	case pctx.SourceLinear:
		if selector == "" {
			return fmt.Errorf("pigeon read linear requires an issue identifier")
		}
		issuesDir := filepath.Join(root.AccountFor(acct).Path(), "issues")
		return readLinearIssue(s, issuesDir, selector)

	default:
		return fmt.Errorf("source %q not yet implemented", src)
	}
}

// DefaultGmailLast is the default number of emails when no filter is specified.
const DefaultGmailLast = 25

// readGWSDateFiles reads JSONL date files from a directory (gmail or calendar),
// deduplicates, filters, and prints raw JSON lines to stdout.
func readGWSDateFiles(s *store.FSStore, dir string, since time.Duration, date string, last int, defaultLast int) error {
	files, err := listDateFilesInDir(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	selected := selectDateFiles(dir, files, since, date)

	// Read and parse all selected files.
	var allLines []modelv1.Line
	var errs []error
	for _, f := range selected {
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
		allLines = filterLinesByTime(allLines, cutoff)
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

	return printLines(allLines, errs)
}

// readDrive finds a drive file directory by fuzzy match and prints its
// content files (markdown/CSV) and comments to stdout.
func readDrive(s *store.FSStore, driveDir paths.DriveDir, selector string) error {
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no drive data for this account")
		}
		return fmt.Errorf("read drive dir: %w", err)
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
		return fmt.Errorf("no drive document matching %q", selector)
	case 1:
		// ok
	default:
		return fmt.Errorf("ambiguous drive document %q — matches: %s", selector, strings.Join(matches, ", "))
	}

	fileDir := driveDir.File(matches[0])

	// Print content files (markdown or CSV).
	contentEntries, err := os.ReadDir(fileDir.Path())
	if err != nil {
		return fmt.Errorf("read drive file dir: %w", err)
	}
	for _, e := range contentEntries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == paths.MarkdownExt || ext == paths.CSVExt {
			data, err := os.ReadFile(filepath.Join(fileDir.Path(), e.Name()))
			if err != nil {
				return fmt.Errorf("read %s: %w", e.Name(), err)
			}
			fmt.Print(string(data))
		}
	}

	// Print comments as raw JSONL.
	lines, err := s.ReadLines(fileDir.CommentsFile())
	if err != nil {
		return fmt.Errorf("read comments: %w", err)
	}
	lines = compact.DedupGWS(lines)
	return printLines(lines, nil)
}

// readMessaging reads a messaging conversation (slack or whatsapp) using
// the existing store layer and formatters.
func readMessaging(s *store.FSStore, acct account.Account, selector string, since time.Duration, date string, last int) error {
	convs, err := s.ListConversations(acct)
	if err != nil {
		return err
	}

	conv, err := fuzzyMatchConversation(convs, selector)
	if err != nil {
		return fmt.Errorf("%s: %w", acct.Display(), err)
	}

	opts := store.ReadOpts{Date: date, Last: last, Since: since}
	df, readErr := s.ReadConversation(acct, conv, opts)
	if df == nil || len(df.Messages) == 0 {
		if readErr != nil {
			return readErr
		}
		fmt.Println("No messages found.")
		return nil
	}

	lines := modelv1.FormatDateFile(df, time.Local, readErr)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// readLinearIssue reads a single linear issue file, deduplicates, and
// prints raw JSON lines to stdout.
func readLinearIssue(s *store.FSStore, issuesDir, selector string) error {
	// Find the issue file by exact or fuzzy match on filename.
	entries, err := os.ReadDir(issuesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no linear issue data")
		}
		return fmt.Errorf("read issues dir: %w", err)
	}

	q := strings.ToLower(selector)
	var matched string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != paths.FileExt {
			continue
		}
		name := strings.TrimSuffix(e.Name(), paths.FileExt)
		if strings.EqualFold(name, selector) {
			matched = e.Name()
			break
		}
		if strings.Contains(strings.ToLower(name), q) {
			if matched != "" {
				return fmt.Errorf("ambiguous linear issue %q", selector)
			}
			matched = e.Name()
		}
	}
	if matched == "" {
		return fmt.Errorf("no linear issue matching %q", selector)
	}

	lines, err := s.ReadLines(paths.DateFile(filepath.Join(issuesDir, matched)))
	if err != nil {
		return fmt.Errorf("read issue %s: %w", matched, err)
	}
	lines = compact.DedupGWS(lines)
	return printLines(lines, nil)
}

// --- helpers ---

func parseTimeFilters(cmd *cobra.Command) (since time.Duration, date string, last int, err error) {
	dateStr, err := cmd.Flags().GetString("date")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get date flag: %w", err)
	}
	lastN, err := cmd.Flags().GetInt("last")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get last flag: %w", err)
	}
	sinceStr, err := cmd.Flags().GetString("since")
	if err != nil {
		return 0, "", 0, fmt.Errorf("get since flag: %w", err)
	}
	var sinceDur time.Duration
	if sinceStr != "" {
		sinceDur, err = timeutil.ParseDuration(sinceStr)
		if err != nil {
			return 0, "", 0, fmt.Errorf("invalid --since value %q: %w", sinceStr, err)
		}
	}
	return sinceDur, dateStr, lastN, nil
}

// listDateFilesInDir returns sorted JSONL date file paths in a directory.
func listDateFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && paths.IsDateFile(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// selectDateFiles picks which date files to read based on time filters.
func selectDateFiles(dir string, files []string, since time.Duration, date string) []string {
	switch {
	case date != "":
		target := filepath.Join(dir, date+paths.FileExt)
		for _, f := range files {
			if f == target {
				return []string{f}
			}
		}
		return nil
	case since > 0:
		cutoffDate := time.Now().Add(-since).Truncate(24 * time.Hour).Format("2006-01-02")
		cutoffPath := filepath.Join(dir, cutoffDate+paths.FileExt)
		i := sort.SearchStrings(files, cutoffPath)
		return files[i:]
	default:
		return files
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

// filterLinesByTime keeps lines with timestamp at or after cutoff.
// Lines without a meaningful timestamp (comments, events with nested times)
// are kept.
func filterLinesByTime(lines []modelv1.Line, cutoff time.Time) []modelv1.Line {
	var out []modelv1.Line
	for _, l := range lines {
		ts := l.Ts()
		if ts.IsZero() || !ts.Before(cutoff) {
			out = append(out, l)
		}
		out = append(out, l)
	}
	return out
}

// fuzzyMatchConversation finds a conversation directory by substring match.
func fuzzyMatchConversation(convs []string, query string) (string, error) {
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
		// Check for exact match.
		for _, m := range matches {
			if strings.EqualFold(m, query) {
				return m, nil
			}
		}
		return "", fmt.Errorf("ambiguous conversation %q — matches: %s", query, strings.Join(matches, ", "))
	}
}

// printLines marshals lines to JSON and prints to stdout. Parse errors
// are appended after the data.
func printLines(lines []modelv1.Line, parseErrs []error) error {
	var errs []error
	for _, l := range lines {
		data, err := modelv1.Marshal(l)
		if err != nil {
			errs = append(errs, fmt.Errorf("marshal line: %w", err))
			continue
		}
		fmt.Println(string(data))
	}
	errs = append(errs, parseErrs...)
	if len(errs) > 0 {
		// Print warnings to stderr so they don't pollute the JSON output.
		for _, e := range errs {
			if e != nil {
				fmt.Fprintf(os.Stderr, "⚠ %s\n", e)
			}
		}
	}
	return nil
}
