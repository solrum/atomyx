package session_test

import (
	"os"
	"syscall"
)

// lockExclusive takes a non-blocking exclusive flock on f.
func lockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
