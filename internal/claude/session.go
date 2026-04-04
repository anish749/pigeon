// Package claude manages Claude Code session files that bind a platform+account
// to a persistent Claude Code session ID.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/anish/claude-msg-utils/internal/daemon"
)

// Session is the data stored in a session file.
type Session struct {
	Platform      string    `yaml:"platform"`
	Account       string    `yaml:"account"`
	SessionID     string    `yaml:"session_id"`
	CWD           string    `yaml:"cwd"`
	Name          string    `yaml:"name"`
	CreatedAt     time.Time `yaml:"created_at"`
	LastDelivered time.Time `yaml:"last_delivered"`
}

// SessionFile manages a session file and its lock. All operations on the
// session file go through this struct, which holds an exclusive lock for
// the lifetime of the SessionFile. Call Close to release the lock.
type SessionFile struct {
	platform string
	account  string
	lock     *os.File
	data     *Session // nil if no session file exists yet
}

// OpenSession acquires an exclusive lock for the given platform+account and
// loads the session data if a file exists. Returns a SessionFile that must
// be closed when done. The lock is non-blocking — returns an error immediately
// if another process holds it.
func OpenSession(platform, account string) (*SessionFile, error) {
	platform = strings.ToLower(platform)
	account = strings.ToLower(account)

	if err := os.MkdirAll(SessionsDir(), 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	lp := lockPath(platform, account)
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open session lock %s: %w", lp, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("session %s/%s is locked by another process", platform, account)
	}

	sf := &SessionFile{
		platform: platform,
		account:  account,
		lock:     f,
	}

	// Load existing session data if the file exists.
	data, err := os.ReadFile(sessionPath(platform, account))
	if err == nil {
		var s Session
		if err := yaml.Unmarshal(data, &s); err != nil {
			sf.Close()
			return nil, fmt.Errorf("parse session file: %w", err)
		}
		sf.data = &s
	} else if !os.IsNotExist(err) {
		sf.Close()
		return nil, fmt.Errorf("read session file: %w", err)
	}

	return sf, nil
}

// Exists returns true if a session file already exists for this platform+account.
func (sf *SessionFile) Exists() bool {
	return sf.data != nil
}

// Data returns the loaded session data, or nil if no session exists yet.
func (sf *SessionFile) Data() *Session {
	return sf.data
}

// Save writes session data to disk. The lock is held throughout.
func (sf *SessionFile) Save(s *Session) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := sessionPath(sf.platform, sf.account)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	sf.data = s
	return nil
}

// Close releases the lock on the session file.
func (sf *SessionFile) Close() error {
	return sf.lock.Close()
}

// SessionsDir returns the directory where session files are stored.
func SessionsDir() string {
	return filepath.Join(daemon.StateDir(), "sessions")
}

// SessionPath returns the full path to a session file for a platform+account.
func SessionPath(platform, account string) string {
	return sessionPath(strings.ToLower(platform), strings.ToLower(account))
}

// SessionName returns the display name for a session (e.g. "slack/tubular").
func SessionName(platform, account string) string {
	return fmt.Sprintf("%s/%s", strings.ToLower(platform), strings.ToLower(account))
}

func sessionPath(platform, account string) string {
	return filepath.Join(SessionsDir(), sessionFileName(platform, account))
}

func sessionFileName(platform, account string) string {
	slug := strings.ReplaceAll(strings.ToLower(account), " ", "-")
	return fmt.Sprintf("%s-%s.yaml", strings.ToLower(platform), slug)
}

func lockPath(platform, account string) string {
	return sessionPath(platform, account) + ".lock"
}

// ListSessions returns all session files from the sessions directory.
func ListSessions() ([]*Session, error) {
	dir := SessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []*Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// UpdateLastDelivered atomically updates the last_delivered timestamp in a session file.
// Acquires a blocking lock, reads current state, updates, writes, releases.
func UpdateLastDelivered(platform, account string, t time.Time) error {
	lp := lockPath(strings.ToLower(platform), strings.ToLower(account))
	if err := os.MkdirAll(SessionsDir(), 0755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer f.Close()

	// Blocking lock — wait until available.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}

	path := sessionPath(strings.ToLower(platform), strings.ToLower(account))
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session file: %w", err)
	}

	var s Session
	if err := yaml.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("parse session file: %w", err)
	}

	s.LastDelivered = t

	out, err := yaml.Marshal(&s)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	return nil
}

// FindSessionByID searches all session files for one matching the given session ID.
func FindSessionByID(sessionID string) (*Session, error) {
	sessions, err := ListSessions()
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		if s.SessionID == sessionID {
			return s, nil
		}
	}
	return nil, nil
}
