package daemon

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// ErrDeviceLocked is returned when a WhatsApp device is already in use by another process.
var ErrDeviceLocked = errors.New("device is locked by another process (is the daemon running?)")

// DeviceLock holds an exclusive advisory lock on a WhatsApp database.
// Call Close to release the lock.
type DeviceLock struct {
	file *os.File
}

// Close releases the device lock.
func (dl *DeviceLock) Close() error {
	return dl.file.Close()
}

// LockDevice acquires an exclusive advisory lock on a WhatsApp database.
// Returns a DeviceLock that must be closed to release the lock.
// Returns ErrDeviceLocked if another process holds the lock.
func LockDevice(dbPath string) (*DeviceLock, error) {
	lockPath := dbPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrDeviceLocked
	}

	return &DeviceLock{file: f}, nil
}
