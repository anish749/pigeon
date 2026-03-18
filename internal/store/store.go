package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DataDir returns the root directory for message data.
// Respects CMU_DATA_DIR env var, defaults to ~/.local/share/cmu/
func DataDir() string {
	if d := os.Getenv("CMU_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "cmu")
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
func ListConversations(platform, account string) ([]Conversation, error) {
	dir := filepath.Join(DataDir(), platform, account)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", dir, err)
	}
	var convs []Conversation
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		convs = append(convs, parseConversationDir(e.Name()))
	}
	return convs, nil
}

// FindConversation finds a conversation by substring match on directory name or display name.
// Returns the first match. Case-insensitive.
func FindConversation(platform, account, query string) (*Conversation, error) {
	convs, err := ListConversations(platform, account)
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

// SearchResult is a single matching line.
type SearchResult struct {
	Platform     string
	Account      string
	Conversation string
	Date         string
	Line         string
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
			convs, err := ListConversations(plat, acct)
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
					for _, line := range lines {
						if strings.Contains(strings.ToLower(line), q) {
							results = append(results, SearchResult{
								Platform:     plat,
								Account:      acct,
								Conversation: conv.DirName,
								Date:         date,
								Line:         line,
							})
						}
					}
				}
			}
		}
	}
	return results, nil
}

// WriteMessage appends a formatted message line to the appropriate date file.
// Creates parent directories if needed. Safe for concurrent use (O_APPEND).
func WriteMessage(platform, account, conversation, sender, text string, ts time.Time) error {
	dir := filepath.Join(DataDir(), platform, account, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create conversation dir %s: %w", dir, err)
	}
	filename := filepath.Join(dir, ts.Format("2006-01-02")+".txt")
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()
	line := fmt.Sprintf("[%s] %s: %s\n", ts.Format("2006-01-02 15:04:05"), sender, text)
	_, err = f.WriteString(line)
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

func filterLinesSince(lines []string, cutoff time.Time) []string {
	// Parse timestamps like [2026-03-16 09:15:02]
	var result []string
	for _, line := range lines {
		if len(line) > 21 && line[0] == '[' {
			ts, err := time.ParseInLocation("2006-01-02 15:04:05", line[1:20], time.Local)
			if err == nil && ts.Before(cutoff) {
				continue
			}
		}
		result = append(result, line)
	}
	return result
}
