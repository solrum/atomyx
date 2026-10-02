package iosdriver_test

import (
	"os"
	"syscall"
	"testing"
)

// holdFlock takes an exclusive flock on path from this test, standing
// in for another process holding it, and returns the function that
// releases it.
func holdFlock(t *testing.T, path string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
