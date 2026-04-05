package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/anish749/pigeon/internal/paths"
)

// IsRunning checks whether the daemon process is alive.
// Cleans up stale PID files.
func IsRunning() bool {
	pid, err := readPID()
	if err != nil {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		// Process doesn't exist — clean stale PID file.
		os.Remove(paths.PIDPath())
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
		os.Remove(paths.PIDPath())
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
	os.Remove(paths.PIDPath())
	os.Remove(paths.SocketPath())

	if err := os.MkdirAll(paths.StateDir(), 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	// The daemon process manages its own log file via lumberjack,
	// so we only need to suppress stdout/stderr here.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open devnull: %w", err)
	}
	defer devNull.Close()

	cmd := exec.Command(exe, "daemon", "_run")
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Detach — we don't wait for the child.
	cmd.Process.Release()

	// Wait for the daemon to write its PID file.
	for range 30 {
		if IsRunning() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start within 3 seconds (check %s)", paths.DaemonLogPath())
}

// Stop sends SIGTERM to the daemon and waits for it to exit.
func Stop() error {
	pid, err := readPID()
	if err != nil {
		return fmt.Errorf("daemon is not running")
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		os.Remove(paths.PIDPath())
		return fmt.Errorf("daemon is not running")
	}

	// Wait up to 5 seconds for it to die.
	for range 50 {
		if err := syscall.Kill(pid, 0); err != nil {
			os.Remove(paths.PIDPath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not stop within 5 seconds (pid %d)", pid)
}

// WritePID writes the current process's PID to the PID file.
// Called by the daemon process itself after starting.
func WritePID() error {
	if err := os.MkdirAll(paths.StateDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(paths.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

// RemovePID removes the PID file and socket. Called on daemon shutdown.
func RemovePID() {
	os.Remove(paths.PIDPath())
	os.Remove(paths.SocketPath())
}

// EnsureRunning starts the daemon if it is not already running.
func EnsureRunning() error {
	if IsRunning() {
		return nil
	}
	return Start()
}

func readPID() (int, error) {
	data, err := os.ReadFile(paths.PIDPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
