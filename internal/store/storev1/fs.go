package storev1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anish749/pigeon/internal/account"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
)

// FSStore implements Store backed by the local filesystem.
type FSStore struct {
	base  string // root data directory
	locks sync.Map
}

// NewFSStore creates a Store rooted at the given base directory.
func NewFSStore(base string) *FSStore {
	return &FSStore{base: base}
}

// acctDir returns the directory for an account: base/platform/account-slug
func (s *FSStore) acctDir(acct account.Account) string {
	return filepath.Join(s.base, acct.Platform, acct.NameSlug())
}

// convDir returns the directory for a conversation.
func (s *FSStore) convDir(acct account.Account, conversation string) string {
	return filepath.Join(s.acctDir(acct), conversation)
}

// fileMu returns the per-file mutex for the given path.
func (s *FSStore) fileMu(path string) *sync.Mutex {
	val, _ := s.locks.LoadOrStore(path, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Append writes a single event line to the appropriate date file.
func (s *FSStore) Append(acct account.Account, conversation string, line modelv1.Line) error {
	ts := line.Ts()
	if ts.IsZero() {
		return fmt.Errorf("append: line has zero timestamp")
	}

	dir := s.convDir(acct, conversation)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create conversation dir: %w", err)
	}

	filename := filepath.Join(dir, ts.UTC().Format("2006-01-02")+".txt")
	return s.appendLine(filename, line)
}

// AppendThread writes a single event line to a thread file.
func (s *FSStore) AppendThread(acct account.Account, conversation, threadTS string, line modelv1.Line) error {
	dir := filepath.Join(s.convDir(acct, conversation), "threads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create threads dir: %w", err)
	}

	filename := filepath.Join(dir, threadTS+".txt")
	return s.appendLine(filename, line)
}

// ReadConversation loads messages from a conversation, applying compaction
// and resolution. Reactions are grouped onto their parent messages.
func (s *FSStore) ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.ResolvedDateFile, error) {
	dir := s.convDir(acct, conversation)
	files, err := listDateFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &modelv1.ResolvedDateFile{}, nil
	}

	var selected []string
	switch {
	case opts.Date != "":
		target := filepath.Join(dir, opts.Date+".txt")
		if fileExists(target) {
			selected = []string{target}
		}
	case opts.Since > 0:
		cutoff := time.Now().Add(-opts.Since)
		for _, f := range files {
			d, err := dateFromFilename(f)
			if err != nil {
				continue
			}
			if !d.Before(cutoff.Truncate(24 * time.Hour)) {
				selected = append(selected, f)
			}
		}
	default:
		today := filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".txt")
		if fileExists(today) {
			selected = []string{today}
		} else {
			selected = []string{files[len(files)-1]}
		}
	}

	merged := &modelv1.DateFile{}
	for _, f := range selected {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		df, parseErr := modelv1.ParseDateFile(data)
		if parseErr != nil {
			slog.Warn("parse date file: some lines skipped", "file", f, "error", parseErr)
		}
		merged.Messages = append(merged.Messages, df.Messages...)
		merged.Reactions = append(merged.Reactions, df.Reactions...)
		merged.Edits = append(merged.Edits, df.Edits...)
		merged.Deletes = append(merged.Deletes, df.Deletes...)
	}

	compacted := compact.Compact(merged)
	resolved := modelv1.Resolve(compacted)

	// Apply --since precise cutoff (file selection is coarse by date).
	if opts.Since > 0 {
		cutoff := time.Now().Add(-opts.Since)
		var filtered []modelv1.ResolvedMsg
		for _, m := range resolved.Messages {
			if !m.Ts.Before(cutoff) {
				filtered = append(filtered, m)
			}
		}
		resolved.Messages = filtered
	}

	if opts.Last > 0 && len(resolved.Messages) > opts.Last {
		resolved.Messages = resolved.Messages[len(resolved.Messages)-opts.Last:]
	}

	return resolved, nil
}

// ReadThread loads a thread file, applying compaction and resolution.
func (s *FSStore) ReadThread(acct account.Account, conversation, threadTS string) (*modelv1.ResolvedThreadFile, error) {
	filename := filepath.Join(s.convDir(acct, conversation), "threads", threadTS+".txt")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read thread %s: %w", threadTS, err)
	}

	tf, parseErr := modelv1.ParseThreadFile(data)
	if parseErr != nil {
		slog.Warn("parse thread file: some lines skipped", "thread", threadTS, "error", parseErr)
	}

	compacted := compact.CompactThread(tf)
	return modelv1.ResolveThread(compacted), nil
}

// Search finds messages matching a query across conversations.
func (s *FSStore) Search(query string, opts SearchOpts) ([]SearchResult, error) {
	q := strings.ToLower(query)
	var results []SearchResult
	var errs []error

	platforms, err := s.ListPlatforms()
	if err != nil {
		return nil, err
	}

	for _, plat := range platforms {
		if opts.Platform != "" && plat != opts.Platform {
			continue
		}
		accounts, err := s.ListAccounts(plat)
		if err != nil {
			errs = append(errs, fmt.Errorf("list accounts %s: %w", plat, err))
			continue
		}
		for _, acctSlug := range accounts {
			if opts.Account != "" && acctSlug != opts.Account {
				continue
			}
			acct := account.NewFromSlug(plat, acctSlug)
			convs, err := s.ListConversations(acct)
			if err != nil {
				errs = append(errs, fmt.Errorf("list conversations %s: %w", acct.Display(), err))
				continue
			}
			for _, conv := range convs {
				convResults, err := s.searchConversation(acct, conv, q, opts)
				if err != nil {
					errs = append(errs, fmt.Errorf("search %s/%s: %w", acct.Display(), conv, err))
					continue
				}
				results = append(results, convResults...)
			}
		}
	}

	return results, errors.Join(errs...)
}

// ListPlatforms returns all platform directories.
func (s *FSStore) ListPlatforms() ([]string, error) {
	return listSubdirs(s.base)
}

// ListAccounts returns all account directories for a platform.
func (s *FSStore) ListAccounts(platform string) ([]string, error) {
	return listSubdirs(filepath.Join(s.base, platform))
}

// ListConversations returns all conversation directories for an account.
func (s *FSStore) ListConversations(acct account.Account) ([]string, error) {
	dir := s.acctDir(acct)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var convs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			convs = append(convs, e.Name())
		}
	}
	return convs, nil
}

// Maintain runs the maintenance pass for an account.
func (s *FSStore) Maintain(acct account.Account) error {
	dir := s.acctDir(acct)
	stateFile := filepath.Join(dir, ".maintenance.json")
	state, err := loadMaintenanceState(stateFile)
	if err != nil {
		return err
	}

	var errs []error
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			errs = append(errs, fmt.Errorf("rel path %s: %w", path, relErr))
			return nil
		}
		mtime := info.ModTime().UTC().Format(time.RFC3339)
		if state[rel] == mtime {
			return nil // unchanged since last maintenance
		}

		if err := s.maintainFile(path); err != nil {
			errs = append(errs, fmt.Errorf("maintain %s: %w", rel, err))
			return nil
		}

		newInfo, statErr := os.Stat(path)
		if statErr != nil {
			errs = append(errs, fmt.Errorf("stat after maintain %s: %w", rel, statErr))
			return nil
		}
		state[rel] = newInfo.ModTime().UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", dir, err)
	}

	if err := saveMaintenanceState(stateFile, state); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// --- internal helpers ---

func (s *FSStore) appendLine(filename string, line modelv1.Line) error {
	mu := s.fileMu(filename)
	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("close after write", "file", filename, "error", cerr)
		}
	}()

	data, merr := modelv1.Marshal(line)
	if merr != nil {
		return fmt.Errorf("marshal line: %w", merr)
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func (s *FSStore) maintainFile(path string) error {
	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Determine if this is a thread file (lives in a threads/ directory).
	if strings.Contains(path, "/threads/") {
		tf, parseErr := modelv1.ParseThreadFile(data)
		if parseErr != nil {
			slog.Warn("parse thread file: some lines skipped", "file", path, "error", parseErr)
		}
		compacted := compact.CompactThread(tf)
		if compacted == nil {
			return os.Remove(path) // parent was deleted
		}
		newData, err := modelv1.MarshalThreadFile(compacted)
		if err != nil {
			return fmt.Errorf("marshal thread: %w", err)
		}
		if string(newData) == string(data) {
			return nil
		}
		return os.WriteFile(path, newData, 0644)
	}

	df, parseErr := modelv1.ParseDateFile(data)
	if parseErr != nil {
		slog.Warn("parse date file: some lines skipped", "file", path, "error", parseErr)
	}
	compacted := compact.Compact(df)
	newData, err := modelv1.MarshalDateFile(compacted)
	if err != nil {
		return fmt.Errorf("marshal date file: %w", err)
	}
	if string(newData) == string(data) {
		return nil
	}
	return os.WriteFile(path, newData, 0644)
}

func (s *FSStore) searchConversation(acct account.Account, conversation, query string, opts SearchOpts) ([]SearchResult, error) {
	dir := s.convDir(acct, conversation)
	files, err := listDateFiles(dir)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	var errs []error
	for _, f := range files {
		if opts.Since > 0 {
			d, dateErr := dateFromFilename(f)
			if dateErr != nil {
				continue
			}
			cutoff := time.Now().Add(-opts.Since)
			if d.Before(cutoff.Truncate(24 * time.Hour)) {
				continue
			}
		}

		data, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", f, err))
			continue
		}
		df, parseErr := modelv1.ParseDateFile(data)
		if parseErr != nil {
			slog.Warn("search: some lines skipped", "file", f, "error", parseErr)
		}

		// Compact and resolve so reactions are grouped onto messages.
		compacted := compact.Compact(df)
		resolved := modelv1.Resolve(compacted)

		var matching []modelv1.ResolvedMsg
		for _, m := range resolved.Messages {
			if strings.Contains(strings.ToLower(m.Text), query) ||
				strings.Contains(strings.ToLower(m.Sender), query) {
				matching = append(matching, m)
			}
		}

		if len(matching) > 0 {
			results = append(results, SearchResult{
				Platform:     acct.Platform,
				Account:      acct.NameSlug(),
				Conversation: conversation,
				Date:         strings.TrimSuffix(filepath.Base(f), ".txt"),
				Messages:     matching,
				MatchCount:   len(matching),
			})
		}
	}
	return results, errors.Join(errs...)
}

var dateFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.txt$`)

func listDateFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && dateFilePattern.MatchString(e.Name()) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func listSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

func dateFromFilename(path string) (time.Time, error) {
	name := strings.TrimSuffix(filepath.Base(path), ".txt")
	t, err := time.Parse("2006-01-02", name)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date from filename %s: %w", path, err)
	}
	return t, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadMaintenanceState(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read maintenance state: %w", err)
	}
	var state map[string]string
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse maintenance state: %w", err)
	}
	return state, nil
}

func saveMaintenanceState(path string, state map[string]string) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal maintenance state: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
