package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

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

	return s.AppendLine(conv.DateFile(ts.UTC().Format("2006-01-02")), line)
}

// AppendThread writes a single event line to a thread file.
func (s *FSStore) AppendThread(acct account.Account, conversation, threadTS string, line modelv1.Line) error {
	conv := s.convDir(acct, conversation)
	if err := os.MkdirAll(conv.ThreadsDir(), 0755); err != nil {
		return fmt.Errorf("create threads dir: %w", err)
	}
	return s.AppendLine(conv.ThreadFile(threadTS), line)
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
			selected = []string{target.Path()}
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
	data, err := os.ReadFile(s.convDir(acct, conversation).ThreadFile(threadTS).Path())
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

// writeMeta writes or overwrites the .meta.json sidecar for a conversation.
func (s *FSStore) writeMeta(acct account.Account, conversation string, meta modelv1.ConvMeta) error {
	conv := s.convDir(acct, conversation)
	if err := os.MkdirAll(conv.Path(), 0755); err != nil {
		return fmt.Errorf("create conversation dir: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	return os.WriteFile(conv.MetaFile().Path(), data, 0644)
}

// WriteMetaIfNotExists writes .meta.json only if it doesn't already exist.
// Returns true if written, false if already present.
func (s *FSStore) WriteMetaIfNotExists(acct account.Account, conversation string, meta modelv1.ConvMeta) (bool, error) {
	mf := s.convDir(acct, conversation).MetaFile()
	if _, err := os.Stat(mf.Path()); err == nil {
		return false, nil
	}
	return true, s.writeMeta(acct, conversation, meta)
}

// ReadMeta reads the .meta.json sidecar for a conversation.
// Returns nil, nil if the file does not exist.
func (s *FSStore) ReadMeta(acct account.Account, conversation string) (*modelv1.ConvMeta, error) {
	data, err := os.ReadFile(s.convDir(acct, conversation).MetaFile().Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read meta: %w", err)
	}
	var meta modelv1.ConvMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta: %w", err)
	}
	return &meta, nil
}

// --- internal helpers ---

// AppendLine appends a single JSONL line to the given log file, creating
// parent directories and the file itself as needed. This is the low-level
// primitive used by Append, AppendThread, and GWS pollers that compute their
// own paths. File writes are serialised by a per-path mutex shared across
// all callers on the same FSStore instance.
func (s *FSStore) AppendLine(df paths.LogFile, line modelv1.Line) error {
	filename := df.Path()
	mu := s.fileMu(filename)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", filename, err)
	}

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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

// WriteLines replaces the contents of the given log file with the supplied
// lines, creating parent directories if needed. Used for data that is always
// fetched as a full snapshot (e.g. Drive comments).
func (s *FSStore) WriteLines(df paths.LogFile, lines []modelv1.Line) error {
	filename := df.Path()
	mu := s.fileMu(filename)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", filename, err)
	}

	var buf []byte
	for _, line := range lines {
		data, err := modelv1.Marshal(line)
		if err != nil {
			return fmt.Errorf("marshal line: %w", err)
		}
		buf = append(buf, data...)
		buf = append(buf, '\n')
	}

	if err := os.WriteFile(filename, buf, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filename, err)
	}
	return nil
}

// ReadLines reads all JSONL lines from the given log file. Returns nil, nil
// if the file doesn't exist. Unparseable lines are collected into the error
// but successfully parsed lines are still returned.
func (s *FSStore) ReadLines(df paths.LogFile) ([]modelv1.Line, error) {
	filename := df.Path()
	f, err := os.Open(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()

	var lines []modelv1.Line
	var errs []error
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		if text == "" {
			continue
		}
		l, err := modelv1.Parse(text)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", lineNum, err))
			continue
		}
		lines = append(lines, l)
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("scan %s: %w", filename, err))
	}
	return lines, errors.Join(errs...)
}

// WriteContent writes raw bytes to a content file (markdown, CSV) produced
// during GWS ingestion. Replaces the file if it already exists.
func (s *FSStore) WriteContent(cf paths.ContentFile, data []byte) error {
	path := cf.Path()
	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// LoadDriveMeta reads Google Drive file metadata from a JSON file.
func (s *FSStore) LoadDriveMeta(mf paths.DriveMetaFile) (*modelv1.DocMeta, error) {
	path := mf.Path()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta %s: %w", path, err)
	}
	var m modelv1.DocMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse meta %s: %w", path, err)
	}
	return &m, nil
}

// SaveDriveMeta writes Google Drive file metadata to a JSON file, creating
// parent directories. After a successful write it removes any stale
// drive-meta-*.json files in the same directory. The write-then-delete order
// ensures metadata is never lost on crash.
func (s *FSStore) SaveDriveMeta(mf paths.DriveMetaFile, m *modelv1.DocMeta) error {
	path := mf.Path()
	dir := mf.Dir()

	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write meta %s: %w", path, err)
	}

	if err := cleanupStaleDriveMeta(dir, mf.Name()); err != nil {
		return fmt.Errorf("cleanup stale meta in %s: %w", dir, err)
	}
	return nil
}

// LoadCursors reads GWS polling cursors for the given account.
// Returns empty Cursors if the file does not exist yet (first-run case).
func (s *FSStore) LoadGWSCursors(acct paths.AccountDir) (*GWSCursors, error) {
	path := acct.SyncCursorsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &GWSCursors{}, nil
		}
		return nil, fmt.Errorf("read cursors %s: %w", path, err)
	}
	var c GWSCursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cursors %s: %w", path, err)
	}
	return &c, nil
}

// SaveCursors writes GWS polling cursors for the given account.
func (s *FSStore) SaveGWSCursors(acct paths.AccountDir, c *GWSCursors) error {
	path := acct.SyncCursorsPath()

	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal cursors: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cursors %s: %w", path, err)
	}
	return nil
}

// LoadLinearCursors reads Linear polling cursors for the given account.
// Returns empty cursors if the file does not exist yet (first-run case).
func (s *FSStore) LoadLinearCursors(acct paths.AccountDir) (*LinearCursors, error) {
	path := acct.SyncCursorsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LinearCursors{}, nil
		}
		return nil, fmt.Errorf("read cursors %s: %w", path, err)
	}
	var c LinearCursors
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cursors %s: %w", path, err)
	}
	return &c, nil
}

// SaveLinearCursors writes Linear polling cursors for the given account.
func (s *FSStore) SaveLinearCursors(acct paths.AccountDir, c *LinearCursors) error {
	path := acct.SyncCursorsPath()

	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", path, err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal cursors: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cursors %s: %w", path, err)
	}
	return nil
}

// RemoveDriveFile deletes any local Drive file directories whose names
// match the given fileID. Drive file directories are named either exactly
// "<fileID>" (empty-title fallback) or "<title-slug>-<fileID>", so a name
// matches if it equals fileID or ends with "-<fileID>".
//
// A missing gdrive directory is not an error — a newly-set-up account
// that hasn't backfilled yet has no local state to clean.
func (s *FSStore) RemoveDriveFile(driveDir paths.DriveDir, fileID string) error {
	entries, err := os.ReadDir(driveDir.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read drive dir %s: %w", driveDir.Path(), err)
	}
	suffix := "-" + fileID
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != fileID && !strings.HasSuffix(name, suffix) {
			continue
		}
		path := driveDir.File(name).Path()
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

// cleanupStaleDriveMeta removes any drive-meta-*.json files in dir except
// keepName. Called after writing a new meta file to remove previous versions.
// AppendPendingDelete records an email ID for deferred deletion.
// The poller calls this when history.list reports a message was deleted.
// The actual removal from date files happens during maintenance.
func (s *FSStore) AppendPendingDelete(gmailDir paths.GmailDir, emailID string) error {
	path := gmailDir.PendingDeletesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create gmail dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open pending deletes: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, emailID); err != nil {
		return fmt.Errorf("write pending delete: %w", err)
	}
	return nil
}

func cleanupStaleDriveMeta(dir, keepName string) error {
	matches, err := filepath.Glob(filepath.Join(dir, paths.DriveMetaFileGlob))
	if err != nil {
		return fmt.Errorf("glob stale meta: %w", err)
	}
	var errs []error
	for _, match := range matches {
		if filepath.Base(match) == keepName {
			continue
		}
		if err := os.Remove(match); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", match, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("remove stale meta files: %w", errors.Join(errs...))
	}
	return nil
}

func (s *FSStore) maintainFile(path string) error {
	mu := s.fileMu(path)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Thread files live under <conversation>/threads/<ts>.jsonl. A date
	// file for a conversation literally named "threads" has the same
	// parent dir name but must be compacted as a date file.
	if paths.IsThreadFile(path) {
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
		if !e.IsDir() && paths.IsDateFile(e.Name()) {
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

func fileExists(df paths.LogFile) bool {
	_, err := os.Stat(df.Path())
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
