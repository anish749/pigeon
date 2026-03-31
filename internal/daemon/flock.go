package daemon

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrDeviceLocked is returned when a WhatsApp device is already in use by another process.
var ErrDeviceLocked = errors.New("device is locked by another process (is the daemon running?)")

// LockDevice acquires an exclusive advisory lock on a WhatsApp database.
// Returns the lock file which must be closed to release the lock.
// Returns ErrDeviceLocked if another process holds the lock.
func LockDevice(dbPath string) (*os.File, error) {
	lockPath := dbPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrDeviceLocked
	}

	return f, nil
}
