// Package paths centralizes all directory and file path resolution for pigeon.
// Every other package imports paths from here instead of computing them independently.
package paths

import (
	"os"
	"path/filepath"
)

// StateDir returns the directory for daemon runtime state (PID file, logs, socket, sessions).
// Respects PIGEON_STATE_DIR env var, defaults to ~/.local/state/pigeon/
func StateDir() string {
	if d := os.Getenv("PIGEON_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "pigeon")
}

// ConfigDir returns the config directory path.
// Respects PIGEON_CONFIG_DIR env var, defaults to ~/.config/pigeon/
func ConfigDir() string {
	if d := os.Getenv("PIGEON_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "pigeon")
}

// DataDir returns the root directory for message data.
// Respects PIGEON_DATA_DIR env var, defaults to ~/.local/share/pigeon/
func DataDir() string {
	if d := os.Getenv("PIGEON_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "pigeon")
}

// --- State dir paths ---

// PIDPath returns the path to the daemon PID file.
func PIDPath() string {
	return filepath.Join(StateDir(), "daemon.pid")
}

// DaemonLogPath returns the path to the daemon log file.
func DaemonLogPath() string {
	return filepath.Join(StateDir(), "daemon.log")
}

// MCPLogPath returns the path to the MCP server log file.
func MCPLogPath() string {
	return filepath.Join(StateDir(), "mcp.log")
}

// SocketPath returns the path to the daemon's unix domain socket.
func SocketPath() string {
	return filepath.Join(StateDir(), "daemon.sock")
}

// SessionsDir returns the directory where session files are stored.
func SessionsDir() string {
	return filepath.Join(StateDir(), "sessions")
}

// LastUpdateCheckPath returns the path to the file that stores the last auto-update check timestamp.
func LastUpdateCheckPath() string {
	return filepath.Join(StateDir(), "last_update_check")
}

// --- Config dir paths ---

// ConfigPath returns the full path to config.yaml.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// --- Data dir paths ---

// DefaultDBPath returns the default path for the WhatsApp SQLite database.
func DefaultDBPath() string {
	return filepath.Join(DataDir(), "whatsapp.db")
}

// DBLockPath returns the path to the WhatsApp database lock file.
func DBLockPath() string {
	return DefaultDBPath() + ".lock"
}

// PlatformDir returns the data directory for a specific platform.
func PlatformDir(platform string) string {
	return filepath.Join(DataDir(), platform)
}

// AccountDir returns the data directory for a specific account.
func AccountDir(platform, account string) string {
	return filepath.Join(DataDir(), platform, account)
}

// ThreadDir returns the path to the threads directory for a conversation.
func ThreadDir(platform, account, conversation string) string {
	return filepath.Join(DataDir(), platform, account, conversation, "threads")
}

// ThreadFilePath returns the path to a specific thread file.
func ThreadFilePath(platform, account, conversation, threadTS string) string {
	return filepath.Join(ThreadDir(platform, account, conversation), threadTS+".txt")
}
