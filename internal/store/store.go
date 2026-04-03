package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// fileLocks provides per-file mutexes so the dedup check + write in WriteMessage
// is atomic. Without this, concurrent writers (e.g. history sync and real-time)
// can both pass the dedup check and produce duplicate lines.
var fileLocks sync.Map

// DataDir returns the root directory for message data.
// Respects PIGEON_DATA_DIR env var, defaults to ~/.local/share/pigeon/
func DataDir() string {
	if d := os.Getenv("PIGEON_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "pigeon")
}

// ListPlatforms returns platform directory names (e.g. "whatsapp", "slack").
func ListPlatforms() ([]string, error) {
	return listSubdirs(DataDir())
}

// ListAccounts returns account directory names for a platform.
func ListAccounts(platform string) ([]string, error) {
	return listSubdirs(filepath.Join(DataDir(), platform))
}

// Conversation represents a conversation directory.
type Conversation struct {
	DirName     string // full directory name, e.g. "+14155559876_Alice"
	DisplayName string // parsed display name, e.g. "Alice"
	Identifier  string // parsed identifier, e.g. "+14155559876" or "#engineering"
}

// ListConversations returns conversations for a platform/account.
// aliases maps directory names to searchable name variants (first entry = display name).
// Pass nil if no name enrichment is needed.
func ListConversations(platform, account string, aliases map[string][]string) ([]Conversation, error) {
	dir := filepath.Join(DataDir(), platform, account)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	var convs []Conversation
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "threads" {
			continue
		}
		c := parseConversationDir(e.Name())
		if names, ok := aliases[c.DirName]; ok && len(names) > 0 {
			c.DisplayName = names[0]
		}
		convs = append(convs, c)
	}
	return convs, nil
}

// FindConversation finds a conversation by substring match on directory name,
// display name, or any alias. Returns the first match. Case-insensitive.
// aliases maps directory names to searchable name variants. Pass nil if not needed.
func FindConversation(platform, account, query string, aliases map[string][]string) (*Conversation, error) {
	convs, err := ListConversations(platform, account, aliases)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	for _, c := range convs {
		if strings.Contains(strings.ToLower(c.DirName), q) ||
			strings.Contains(strings.ToLower(c.DisplayName), q) ||
			strings.Contains(strings.ToLower(c.Identifier), q) {
			return &c, nil
		}
		// Also match against all aliases (push name, contact name, etc.)
		for _, name := range aliases[c.DirName] {
			if strings.Contains(strings.ToLower(name), q) {
				return &c, nil
			}
		}
	}
	return nil, fmt.Errorf("no conversation matching %q in %s/%s", query, platform, account)
}

// ReadOpts controls which messages to read.
type ReadOpts struct {
	Date  string        // specific date, e.g. "2026-03-15"
	Last  int           // last N lines across all files
	Since time.Duration // messages from the last duration
}

// ReadMessages reads messages from a conversation.
// If no opts filter is set, reads today's messages.
func ReadMessages(platform, account, conversation string, opts ReadOpts) ([]string, error) {
	dir := filepath.Join(DataDir(), platform, account, conversation)

	files, err := listTxtFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Filter by specific date
	if opts.Date != "" {
		target := opts.Date + ".txt"
		for _, f := range files {
			if filepath.Base(f) == target {
				return readFileLines(f)
			}
		}
		return nil, fmt.Errorf("no messages for date %s", opts.Date)
	}

	// Filter by -since duration
	if opts.Since > 0 {
		cutoff := time.Now().Add(-opts.Since)
		cutoffDate := cutoff.Format("2006-01-02")
		var lines []string
		for _, f := range files {
			base := strings.TrimSuffix(filepath.Base(f), ".txt")
			if base >= cutoffDate {
				fl, err := readFileLines(f)
				if err != nil {
					return nil, err
				}
				// For the cutoff date file, filter lines by timestamp
				if base == cutoffDate {
					fl = filterLinesSince(fl, cutoff)
				}
				lines = append(lines, fl...)
			}
		}
		return lines, nil
	}

	// Filter by -last N lines
	if opts.Last > 0 {
		return readLastNLines(files, opts.Last)
	}

	// Default: today's messages
	today := time.Now().Format("2006-01-02") + ".txt"
	for _, f := range files {
		if filepath.Base(f) == today {
			return readFileLines(f)
		}
	}
	// If no today file, return last file
	return readFileLines(files[len(files)-1])
}

// annotateThreadParent inserts a [thread:ts] marker after the timestamp in a message line.
// Input:  [2026-03-16 09:15:02 +00:00] Alice: hello
// Output: [2026-03-16 09:15:02 +00:00] [thread:1711568938.123456] Alice: hello
func annotateThreadParent(line, threadTS string) string {
	idx := strings.Index(line, "] ")
	if idx < 0 {
		return line
	}
	return line[:idx+2] + "[thread:" + threadTS + "] " + line[idx+2:]
}

// InterleaveThreads enriches channel message lines with thread content.
// For each channel line that matches a thread parent (by comparing text content),
// the thread replies and channel context are inserted after the parent line.
// Parent lines are annotated with [thread:ts] so consumers can reference the thread.
func InterleaveThreads(platform, account, conversation string, lines []string) []string {
	threadsDir := ThreadDir(platform, account, conversation)
	if _, err := os.Stat(threadsDir); os.IsNotExist(err) {
		return lines
	}

	threadTSs, err := ListThreads(platform, account, conversation)
	if err != nil || len(threadTSs) == 0 {
		return lines
	}

	// Read the first line (parent) of each thread file and map it to the thread TS
	parentToThread := make(map[string]string) // parent line text → thread TS
	for _, ts := range threadTSs {
		threadLines, err := ReadThread(platform, account, conversation, ts)
		if err != nil || len(threadLines) == 0 {
			continue
		}
		parentToThread[threadLines[0]] = ts
	}
	if len(parentToThread) == 0 {
		return lines
	}

	var result []string
	for _, line := range lines {
		ts, ok := parentToThread[line]
		if !ok {
			result = append(result, line)
			continue
		}

		result = append(result, annotateThreadParent(line, ts))

		// Read thread file and append content after parent (skip first line = parent)
		threadLines, err := ReadThread(platform, account, conversation, ts)
		if err != nil || len(threadLines) <= 1 {
			continue
		}
		for _, tl := range threadLines[1:] {
			result = append(result, tl)
		}
	}
	return result
}

// AppendActiveThreads appends full thread content for threads that had activity
// within the given cutoff time but whose parent wasn't already shown in the channel
// lines. This ensures thread replies are visible in time-filtered reads even when
// the parent message is older than the time window.
func AppendActiveThreads(platform, account, conversation string, lines []string, since time.Duration) []string {
	if since <= 0 {
		return lines
	}

	threadTSs, err := ListThreads(platform, account, conversation)
	if err != nil || len(threadTSs) == 0 {
		return lines
	}

	cutoff := time.Now().Add(-since)

	// Build set of thread TSs already interleaved (their parent was in channel lines)
	shownParents := make(map[string]struct{})
	for _, ts := range threadTSs {
		threadLines, err := ReadThread(platform, account, conversation, ts)
		if err != nil || len(threadLines) == 0 {
			continue
		}
		for _, line := range lines {
			if line == threadLines[0] {
				shownParents[ts] = struct{}{}
				break
			}
		}
	}

	// Find threads with recent activity that weren't already shown
	var activeThreads []struct {
		ts    string
		lines []string
	}
	for _, ts := range threadTSs {
		if _, shown := shownParents[ts]; shown {
			continue
		}
		threadLines, err := ReadThread(platform, account, conversation, ts)
		if err != nil || len(threadLines) == 0 {
			continue
		}
		// Check if any line in the thread is within the time window
		hasRecent := false
		for _, tl := range threadLines {
			if lineTS := parseLineTime(tl); !lineTS.IsZero() && !lineTS.Before(cutoff) {
				hasRecent = true
				break
			}
		}
		if hasRecent {
			activeThreads = append(activeThreads, struct {
				ts    string
				lines []string
			}{ts, threadLines})
		}
	}

	if len(activeThreads) == 0 {
		return lines
	}

	for _, t := range activeThreads {
		lines = append(lines, "") // blank line separator
		// Annotate the parent line (first line) with thread timestamp
		lines = append(lines, annotateThreadParent(t.lines[0], t.ts))
		lines = append(lines, t.lines[1:]...)
	}
	return lines
}

// parseLineTime parses the timestamp from a message line (handles indented lines too).
func parseLineTime(line string) time.Time {
	trimmed := strings.TrimLeft(line, " ")
	if len(trimmed) < 28 || trimmed[0] != '[' {
		return time.Time{}
	}
	ts, err := time.Parse("2006-01-02 15:04:05 -07:00", trimmed[1:27])
	if err != nil {
		return time.Time{}
	}
	return ts
}

// contextLines is the number of messages to show before and after each match.
const contextLines = 3

// SearchResult is a section of conversation around one or more matches.
type SearchResult struct {
	Platform     string
	Account      string
	Conversation string
	Date         string
	Lines        []string // section of chat (context + matches)
	MatchCount   int      // number of matching lines in this section
}

// SearchMessages searches for a query string across message files.
func SearchMessages(query, platform, account string, since time.Duration) ([]SearchResult, error) {
	root := DataDir()
	q := strings.ToLower(query)

	var platforms []string
	if platform != "" {
		platforms = []string{platform}
	} else {
		var err error
		platforms, err = ListPlatforms()
		if err != nil {
			return nil, err
		}
	}

	var cutoffDate string
	if since > 0 {
		cutoffDate = time.Now().Add(-since).Format("2006-01-02")
	}

	var results []SearchResult
	for _, plat := range platforms {
		var accounts []string
		if account != "" {
			accounts = []string{account}
		} else {
			var err error
			accounts, err = ListAccounts(plat)
			if err != nil {
				continue
			}
		}
		for _, acct := range accounts {
			convs, err := ListConversations(plat, acct, nil)
			if err != nil {
				continue
			}
			for _, conv := range convs {
				convDir := filepath.Join(root, plat, acct, conv.DirName)
				files, err := listTxtFiles(convDir)
				if err != nil {
					continue
				}
				for _, f := range files {
					date := strings.TrimSuffix(filepath.Base(f), ".txt")
					if cutoffDate != "" && date < cutoffDate {
						continue
					}
					lines, err := readFileLines(f)
					if err != nil {
						continue
					}
					for _, sec := range buildSections(lines, q) {
						results = append(results, SearchResult{
							Platform:     plat,
							Account:      acct,
							Conversation: conv.DirName,
							Date:         date,
							Lines:        sec.lines,
							MatchCount:   sec.matchCount,
						})
					}
				}
				// Also search thread files for this conversation
				threadFiles, err := listThreadTxtFiles(convDir)
				if err == nil {
					for _, f := range threadFiles {
						threadTS := strings.TrimSuffix(filepath.Base(f), ".txt")
						lines, err := readFileLines(f)
						if err != nil {
							continue
						}
						// Check cutoff by parsing the first line's timestamp
						if cutoffDate != "" && len(lines) > 0 {
							if firstTS := parseLineDate(lines[0]); firstTS != "" && firstTS < cutoffDate {
								continue
							}
						}
						for _, sec := range buildSections(lines, q) {
							results = append(results, SearchResult{
								Platform:     plat,
								Account:      acct,
								Conversation: conv.DirName,
								Date:         "thread:" + threadTS,
								Lines:        sec.lines,
								MatchCount:   sec.matchCount,
							})
						}
					}
				}
			}
		}
	}
	return results, nil
}

// parseLineDate extracts the YYYY-MM-DD portion from a message line.
func parseLineDate(line string) string {
	if len(line) > 11 && line[0] == '[' {
		return line[1:11]
	}
	// Handle indented thread reply lines
	trimmed := strings.TrimLeft(line, " ")
	if len(trimmed) > 11 && trimmed[0] == '[' {
		return trimmed[1:11]
	}
	return ""
}

// WriteMessage appends a formatted message line to the appropriate date file.
// Deduplicates by skipping writes if the exact line already exists in the file.
// Creates parent directories if needed. Safe for concurrent use (O_APPEND).
func WriteMessage(platform, account, conversation, sender, text string, ts time.Time) error {
	dir := filepath.Join(DataDir(), platform, account, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create conversation dir %s: %w", dir, err)
	}
	filename := filepath.Join(dir, ts.Format("2006-01-02")+".txt")
	line := fmt.Sprintf("[%s] %s: %s", ts.Format("2006-01-02 15:04:05 -07:00"), sender, text)
	return appendDedup(filename, line)
}

// ThreadDir returns the path to the threads directory for a conversation.
func ThreadDir(platform, account, conversation string) string {
	return filepath.Join(DataDir(), platform, account, conversation, "threads")
}

// ThreadFilePath returns the path to a specific thread file.
func ThreadFilePath(platform, account, conversation, threadTS string) string {
	return filepath.Join(ThreadDir(platform, account, conversation), threadTS+".txt")
}

// WriteThreadMessage appends a message to a thread file. If isReply is true,
// the line is indented with two spaces. Deduplicates like WriteMessage.
func WriteThreadMessage(platform, account, conversation, threadTS, sender, text string, ts time.Time, isReply bool) error {
	dir := ThreadDir(platform, account, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create threads dir %s: %w", dir, err)
	}
	filename := filepath.Join(dir, threadTS+".txt")
	line := fmt.Sprintf("[%s] %s: %s", ts.Format("2006-01-02 15:04:05 -07:00"), sender, text)
	if isReply {
		line = "  " + line
	}
	return appendDedup(filename, line)
}

// WriteThreadContext appends a channel context line to a thread file.
// These lines appear after a "--- channel context ---" separator and represent
// messages posted in the channel shortly after the thread parent.
func WriteThreadContext(platform, account, conversation, threadTS, sender, text string, ts time.Time) error {
	dir := ThreadDir(platform, account, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create threads dir %s: %w", dir, err)
	}
	filename := filepath.Join(dir, threadTS+".txt")
	line := fmt.Sprintf("[%s] %s: %s", ts.Format("2006-01-02 15:04:05 -07:00"), sender, text)
	return appendDedup(filename, line)
}

// EnsureThreadContextSeparator writes the "--- channel context ---" separator
// to a thread file if it doesn't already exist.
func EnsureThreadContextSeparator(platform, account, conversation, threadTS string) error {
	dir := ThreadDir(platform, account, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create threads dir %s: %w", dir, err)
	}
	filename := filepath.Join(dir, threadTS+".txt")
	return appendDedup(filename, "--- channel context ---")
}

// ReadThread reads all lines from a thread file.
func ReadThread(platform, account, conversation, threadTS string) ([]string, error) {
	filename := ThreadFilePath(platform, account, conversation, threadTS)
	return readFileLines(filename)
}

// ListThreads returns thread timestamps for a conversation by listing thread files.
func ListThreads(platform, account, conversation string) ([]string, error) {
	dir := ThreadDir(platform, account, conversation)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var threads []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			threads = append(threads, strings.TrimSuffix(e.Name(), ".txt"))
		}
	}
	return threads, nil
}

// appendDedup appends a line to a file, skipping if it already exists.
// Safe for concurrent use via per-file locks.
func appendDedup(filename, line string) error {
	lockVal, _ := fileLocks.LoadOrStore(filename, &sync.Mutex{})
	mu := lockVal.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	if existing, err := os.ReadFile(filename); err == nil {
		for _, existingLine := range strings.Split(string(existing), "\n") {
			if existingLine == line {
				return nil
			}
		}
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// DefaultDBPath returns the default path for the WhatsApp SQLite database.
func DefaultDBPath() string {
	return filepath.Join(DataDir(), "whatsapp.db")
}

// --- helpers ---

func listSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func parseConversationDir(name string) Conversation {
	// Formats: "+14155559876_Alice", "#engineering", "@dave"
	c := Conversation{DirName: name}
	if idx := strings.Index(name, "_"); idx > 0 {
		c.Identifier = name[:idx]
		c.DisplayName = name[idx+1:]
	} else {
		c.Identifier = name
		c.DisplayName = name
	}
	return c
}

func listTxtFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files) // sorts by date since filenames are YYYY-MM-DD.txt
	return files, nil
}

// listThreadTxtFiles returns all .txt files in the threads subdirectory.
func listThreadTxtFiles(dir string) ([]string, error) {
	threadsDir := filepath.Join(dir, "threads")
	entries, err := os.ReadDir(threadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			files = append(files, filepath.Join(threadsDir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func readLastNLines(files []string, n int) ([]string, error) {
	// Read files in reverse order until we have enough lines
	var all []string
	for i := len(files) - 1; i >= 0 && len(all) < n; i-- {
		lines, err := readFileLines(files[i])
		if err != nil {
			return nil, err
		}
		all = append(lines, all...) // prepend
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

type section struct {
	lines      []string
	matchCount int
}

// buildSections finds lines matching the query and groups them into sections
// with contextLines of surrounding messages. Overlapping windows are merged.
func buildSections(lines []string, q string) []section {
	// Find matching indices.
	var matchIdx []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), q) {
			matchIdx = append(matchIdx, i)
		}
	}
	if len(matchIdx) == 0 {
		return nil
	}

	// Build merged ranges: [start, end) with context.
	type rng struct{ start, end, matches int }
	var ranges []rng
	for _, idx := range matchIdx {
		start := idx - contextLines
		if start < 0 {
			start = 0
		}
		end := idx + contextLines + 1
		if end > len(lines) {
			end = len(lines)
		}
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end {
			// Merge with previous range.
			ranges[len(ranges)-1].end = end
			ranges[len(ranges)-1].matches++
		} else {
			ranges = append(ranges, rng{start, end, 1})
		}
	}

	// Build sections from ranges.
	sections := make([]section, len(ranges))
	for i, r := range ranges {
		sections[i] = section{
			lines:      append([]string(nil), lines[r.start:r.end]...),
			matchCount: r.matches,
		}
	}
	return sections
}

func filterLinesSince(lines []string, cutoff time.Time) []string {
	// Parse timestamps like [2026-03-16 09:15:02 -04:00]
	var result []string
	for _, line := range lines {
		if len(line) > 28 && line[0] == '[' {
			ts, err := time.Parse("2006-01-02 15:04:05 -07:00", line[1:27])
			if err == nil && ts.Before(cutoff) {
				continue
			}
		}
		result = append(result, line)
	}
	return result
}
