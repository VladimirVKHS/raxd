package keystore

import (
	"fmt"
	"os"
	"syscall"
)

// lockMode distinguishes between shared (read) and exclusive (write) advisory locks.
type lockMode int

const (
	lockShared    lockMode = syscall.LOCK_SH
	lockExclusive lockMode = syscall.LOCK_EX
)

// acquireLock opens (or for exclusive mode: creates) the given path and applies an advisory flock.
// For read operations (lockShared): opens existing file; returns (nil, nil) if file does not exist.
// For write operations (lockExclusive): creates the file if absent (O_RDWR|O_CREATE).
//
// SR-23: flock is acquired around every operation and released via releaseLock.
// Caller MUST call releaseLock(f) when done — even on error paths (use defer).
//
// LOCK_NB is NOT used: we block rather than time out, avoiding races on busy systems.
func acquireLock(path string, mode lockMode) (*os.File, error) {
	var (
		f   *os.File
		err error
	)

	if mode == lockShared {
		// Read-only operations: open existing file; absent file = empty store (SR-22).
		f, err = os.OpenFile(path, os.O_RDONLY, 0o600)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil //nolint:nilnil // signal: file absent → empty store
			}
			return nil, fmt.Errorf("cannot open key store: %w", err)
		}
	} else {
		// Write operations: open or create the file (O_RDWR|O_CREATE).
		// SR-20: file will be written atomically via temp→rename; this open is just for the lock fd.
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("cannot open key store: %w", err)
		}
	}

	if err := syscall.Flock(int(f.Fd()), int(mode)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("key store is locked by another process: %w", err)
	}
	return f, nil
}

// releaseLock releases the flock and closes the file.
// SR-23: lock is always released, including on error paths (call via defer).
func releaseLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}
