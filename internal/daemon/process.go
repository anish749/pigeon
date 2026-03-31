package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// StateDir returns the directory for daemon runtime state (PID file, logs).
// Respects PIGEON_STATE_DIR env var, defaults to ~/.local/state/pigeon/
func StateDir() string {
	if d := os.Getenv("PIGEON_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "pigeon")
}

// PIDPath returns the path to the daemon PID file.
func PIDPath() string {
	return filepath.Join(StateDir(), "daemon.pid")
}

// LogPath returns the path to the daemon log file.
func LogPath() string {
	return filepath.Join(StateDir(), "daemon.log")
}

// IsRunning checks whether the daemon process is alive.
// Cleans up stale PID files.
func IsRunning() bool {
	pid, err := readPID()
	if err != nil {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		// Process doesn't exist — clean stale PID file.
		os.Remove(PIDPath())
		return false
	}
	return true
}

// Status returns the daemon's running state and PID.
func Status() (running bool, pid int) {
	p, err := readPID()
	if err != nil {
		return false, 0
	}
	if err := syscall.Kill(p, 0); err != nil {
		os.Remove(PIDPath())
		return false, 0
	}
	return true, p
}

// Start launches the daemon in the background by re-executing the current
// binary with "daemon _run". Waits up to 3 seconds for the PID file to
// appear as confirmation.
func Start() error {
	if IsRunning() {
		return fmt.Errorf("daemon is already running")
	}

	// Clean stale files.
	os.Remove(PIDPath())

	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	logFile, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.Command(exe, "daemon", "_run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}

	// Detach — we don't wait for the child.
	cmd.Process.Release()
	logFile.Close()

	// Wait for the daemon to write its PID file.
	for range 30 {
		if IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start within 3 seconds (check %s)", LogPath())
}

// Stop sends SIGTERM to the daemon and waits for it to exit.
func Stop() error {
	pid, err := readPID()
	if err != nil {
		return fmt.Errorf("daemon is not running")
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		os.Remove(PIDPath())
		return fmt.Errorf("daemon is not running")
	}

	// Wait up to 5 seconds for it to die.
	for range 50 {
		if err := syscall.Kill(pid, 0); err != nil {
			os.Remove(PIDPath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not stop within 5 seconds (pid %d)", pid)
}

// WritePID writes the current process's PID to the PID file.
// Called by the daemon process itself after starting.
func WritePID() error {
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

// RemovePID removes the PID file. Called on daemon shutdown.
func RemovePID() {
	os.Remove(PIDPath())
}

// EnsureRunning starts the daemon if it is not already running.
func EnsureRunning() error {
	if IsRunning() {
		return nil
	}
	return Start()
}

func readPID() (int, error) {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
