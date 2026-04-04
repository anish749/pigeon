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

// updateLastDelivered updates the last_delivered timestamp and saves to disk.
// The lock is held throughout.
func (sf *SessionFile) updateLastDelivered(t time.Time) error {
	if sf.data == nil {
		return fmt.Errorf("no session data to update")
	}
	sf.data.LastDelivered = t
	return sf.Save(sf.data)
}

// Close releases the lock on the session file.
func (sf *SessionFile) Close() error {
	return sf.lock.Close()
}

// ListAllSessions scans the sessions directory and returns data from each
// session file. Each file is opened with a lock, read, and closed.
func ListAllSessions() ([]*Session, error) {
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

func UpdateLastDelivered(platform, account string, t time.Time) error {
	sf, err := OpenSession(platform, account)
	if err != nil {
		return err
	}
	defer sf.Close()
	return sf.updateLastDelivered(t)	
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
