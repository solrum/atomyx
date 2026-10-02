package flock

import (
	"errors"
	"os"
	"syscall"
)

// TryExclusive takes an exclusive lock on f without blocking. It
// returns an error for which IsContended is true when another open file
// description holds the lock.
func TryExclusive(f *os.File) error {
	return apply(f, syscall.LOCK_EX|syscall.LOCK_NB)
}

// Exclusive takes an exclusive lock on f and blocks until it is
// granted.
func Exclusive(f *os.File) error {
	return apply(f, syscall.LOCK_EX)
}

// TryShared takes a shared lock on f without blocking. Any number of
// open file descriptions may hold a shared lock at once, and none can
// while another holds an exclusive one. It returns an error for which
// IsContended is true when an exclusive lock is held.
func TryShared(f *os.File) error {
	return apply(f, syscall.LOCK_SH|syscall.LOCK_NB)
}

// Unlock releases the lock f holds. Closing f releases it too.
func Unlock(f *os.File) error {
	return apply(f, syscall.LOCK_UN)
}

// IsContended reports whether err means the lock is held elsewhere.
func IsContended(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}

// SameFile reports whether f still is the file at path. It is false
// when the path was replaced or removed since f was opened.
func SameFile(f *os.File, path string) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	pi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(fi, pi)
}

// apply goes through SyscallConn so the descriptor is never switched
// to blocking mode, and it retries when a signal interrupts the call.
func apply(f *os.File, how int) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var callErr error
	ctrlErr := rc.Control(func(fd uintptr) {
		for {
			callErr = syscall.Flock(int(fd), how)
			if !errors.Is(callErr, syscall.EINTR) {
				return
			}
		}
	})
	if ctrlErr != nil {
		return ctrlErr
	}
	return callErr
}
