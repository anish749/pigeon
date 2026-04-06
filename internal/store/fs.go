package store

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
	"github.com/anish749/pigeon/internal/paths"
	"github.com/anish749/pigeon/internal/store/modelv1"
	"github.com/anish749/pigeon/internal/store/modelv1/compact"
)

// FSStore implements Store backed by the local filesystem.
type FSStore struct {
	root  paths.DataRoot
	locks sync.Map
}

// NewFSStore creates a Store rooted at the given data root.
func NewFSStore(root paths.DataRoot) *FSStore {
	return &FSStore{root: root}
}

func (s *FSStore) convDir(acct account.Account, conversation string) paths.ConversationDir {
	return s.root.AccountFor(acct).Conversation(conversation)
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

	conv := s.convDir(acct, conversation)
	if err := os.MkdirAll(conv.Path(), 0755); err != nil {
		return fmt.Errorf("create conversation dir: %w", err)
	}

	return s.appendLine(conv.DateFile(ts.UTC().Format("2006-01-02")), line)
}

// AppendThread writes a single event line to a thread file.
func (s *FSStore) AppendThread(acct account.Account, conversation, threadTS string, line modelv1.Line) error {
	conv := s.convDir(acct, conversation)
	if err := os.MkdirAll(conv.ThreadsDir(), 0755); err != nil {
		return fmt.Errorf("create threads dir: %w", err)
	}
	return s.appendLine(conv.ThreadFile(threadTS), line)
}

// ReadConversation loads messages from a conversation, applying compaction
// and resolution. Reactions are grouped onto their parent messages.
func (s *FSStore) ReadConversation(acct account.Account, conversation string, opts ReadOpts) (*modelv1.ResolvedDateFile, error) {
	conv := s.convDir(acct, conversation)
	files, err := listDateFiles(conv.Path())
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return &modelv1.ResolvedDateFile{}, nil
	}

	var selected []string
	switch {
	case opts.Date != "":
		target := conv.DateFile(opts.Date)
		if fileExists(target) {
			selected = []string{target}
		}
	case opts.Since > 0:
		cutoffDate := time.Now().Add(-opts.Since).Truncate(24 * time.Hour).Format("2006-01-02")
		i := sort.SearchStrings(files, filepath.Join(conv.Path(), cutoffDate+paths.FileExt))
		selected = files[i:]
	case opts.Last > 0:
		// For --last N, read all files so we can slice after compaction.
		selected = files
	default:
		// No filter specified: return the last 25 messages.
		selected = files
		opts.Last = 25
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

	// Interleave thread replies after their parent message.
	resolved, interleaveErr := s.interleaveThreads(acct, conversation, resolved)

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

	// Return partial data + error. The caller gets whatever threads
	// succeeded and can decide how to handle the error.
	return resolved, interleaveErr
}

// ThreadExists checks if a thread file exists for the given thread timestamp.
func (s *FSStore) ThreadExists(acct account.Account, conversation, threadTS string) bool {
	return fileExists(s.convDir(acct, conversation).ThreadFile(threadTS))
}

// ReadThread loads a thread file, applying compaction and resolution.
func (s *FSStore) ReadThread(acct account.Account, conversation, threadTS string) (*modelv1.ResolvedThreadFile, error) {
	data, err := os.ReadFile(s.convDir(acct, conversation).ThreadFile(threadTS))
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

// interleaveThreads reads thread files for the conversation and splices
// replies into the resolved output after their parent message, matched by ID.
func (s *FSStore) interleaveThreads(acct account.Account, conversation string, resolved *modelv1.ResolvedDateFile) (*modelv1.ResolvedDateFile, error) {
	entries, err := os.ReadDir(s.convDir(acct, conversation).ThreadsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return resolved, nil
		}
		return resolved, fmt.Errorf("read threads dir: %w", err)
	}

	var errs []error
	threads := make(map[string]*modelv1.ResolvedThreadFile)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), paths.FileExt) {
			continue
		}
		threadTS := strings.TrimSuffix(e.Name(), paths.FileExt)
		tf, err := s.ReadThread(acct, conversation, threadTS)
		if err != nil {
			errs = append(errs, fmt.Errorf("read thread %s: %w", threadTS, err))
			continue
		}
		if tf == nil {
			continue
		}
		threads[tf.Parent.ID] = tf
	}

	if len(threads) == 0 {
		return resolved, errors.Join(errs...)
	}

	var result []modelv1.ResolvedMsg
	for _, m := range resolved.Messages {
		result = append(result, m)
		if tf, ok := threads[m.ID]; ok {
			for _, r := range tf.Replies {
				r.Reply = true
				result = append(result, r)
			}
		}
	}

	return &modelv1.ResolvedDateFile{Messages: result}, errors.Join(errs...)
}

// ListPlatforms returns all platform directories.
func (s *FSStore) ListPlatforms() ([]string, error) {
	return listSubdirs(s.root.Path())
}

// ListAccounts returns all account directories for a platform.
func (s *FSStore) ListAccounts(platform string) ([]string, error) {
	return listSubdirs(s.root.Platform(platform).Path())
}

// ListConversations returns all conversation directories for an account.
func (s *FSStore) ListConversations(acct account.Account) ([]string, error) {
	dir := s.root.AccountFor(acct).Path()
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
	ad := s.root.AccountFor(acct)
	dir := ad.Path()
	stateFile := ad.MaintenancePath()
	state, err := loadMaintenanceState(stateFile)
	if err != nil {
		return err
	}

	var errs []error
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, paths.FileExt) {
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

	// Determine if this is a thread file (parent directory is "threads").
	if filepath.Base(filepath.Dir(path)) == paths.ThreadsSubdir {
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

var dateFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

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
