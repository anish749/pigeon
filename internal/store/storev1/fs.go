package storev1

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	ts := lineTimestamp(line)
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

// ReadConversation loads messages from a conversation with in-memory compaction.
func (s *FSStore) ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.DateFile, error) {
	dir := s.convDir(acct, conversation)
	files, err := listDateFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &modelv1.DateFile{}, nil
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
				continue // non-date files in the directory
			}
			if !d.Before(cutoff.Truncate(24 * time.Hour)) {
				selected = append(selected, f)
			}
		}
	default:
		// Default: today's file, or last file if today doesn't exist
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
		df, err := modelv1.ParseDateFile(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		merged.Messages = append(merged.Messages, df.Messages...)
		merged.Reactions = append(merged.Reactions, df.Reactions...)
		merged.Edits = append(merged.Edits, df.Edits...)
		merged.Deletes = append(merged.Deletes, df.Deletes...)
	}

	result := compact.Compact(merged)

	if opts.Last > 0 && len(result.Messages) > opts.Last {
		result.Messages = result.Messages[len(result.Messages)-opts.Last:]
	}

	return result, nil
}

// ReadThread loads a thread file with in-memory compaction.
func (s *FSStore) ReadThread(acct account.Account, conversation, threadTS string) (*modelv1.ThreadFile, error) {
	filename := filepath.Join(s.convDir(acct, conversation), "threads", threadTS+".txt")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read thread %s: %w", threadTS, err)
	}

	tf, err := modelv1.ParseThreadFile(data)
	if err != nil {
		return nil, fmt.Errorf("parse thread %s: %w", threadTS, err)
	}

	return compact.CompactThread(tf), nil
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
			acct := account.New(plat, acctSlug)
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
	defer f.Close()

	_, err = f.WriteString(modelv1.Marshal(line) + "\n")
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
		tf, err := modelv1.ParseThreadFile(data)
		if err != nil {
			return err
		}
		compacted := compact.CompactThread(tf)
		if compacted == nil {
			return os.Remove(path) // parent was deleted
		}
		newData := modelv1.MarshalThreadFile(compacted)
		if string(newData) == string(data) {
			return nil
		}
		return os.WriteFile(path, newData, 0644)
	}

	df, err := modelv1.ParseDateFile(data)
	if err != nil {
		return err
	}
	compacted := compact.Compact(df)
	newData := modelv1.MarshalDateFile(compacted)
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
		df, err := modelv1.ParseDateFile(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("parse %s: %w", f, err))
			continue
		}

		var matching []modelv1.Line
		for _, m := range df.Messages {
			if strings.Contains(strings.ToLower(m.Text), query) ||
				strings.Contains(strings.ToLower(m.Sender), query) {
				matching = append(matching, modelv1.Line{Type: modelv1.LineMessage, Msg: &m})
			}
		}

		if len(matching) > 0 {
			results = append(results, SearchResult{
				Platform:     acct.Platform,
				Account:      acct.NameSlug(),
				Conversation: conversation,
				Date:         strings.TrimSuffix(filepath.Base(f), ".txt"),
				Lines:        matching,
				MatchCount:   len(matching),
			})
		}
	}
	return results, errors.Join(errs...)
}

func lineTimestamp(l modelv1.Line) time.Time {
	switch l.Type {
	case modelv1.LineMessage:
		return l.Msg.Ts
	case modelv1.LineReaction, modelv1.LineUnreaction:
		return l.React.Ts
	case modelv1.LineEdit:
		return l.Edit.Ts
	case modelv1.LineDelete:
		return l.Delete.Ts
	default:
		return time.Time{}
	}
}

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
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
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
