package flock

import (
	"os"
	"path/filepath"
	"testing"
)

func openLockFile(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestTryExclusive_SecondDescriptorIsContendedUntilUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, b := openLockFile(t, path), openLockFile(t, path)

	if err := TryExclusive(a); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	err := TryExclusive(b)
	if !IsContended(err) {
		t.Fatalf("second lock = %v, want contended", err)
	}
	if err := Unlock(a); err != nil {
		t.Fatal(err)
	}
	if err := TryExclusive(b); err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
}

func TestTryExclusive_CloseReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, b := openLockFile(t, path), openLockFile(t, path)
	if err := TryExclusive(a); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := TryExclusive(b); err != nil {
		t.Fatalf("lock after close: %v", err)
	}
}

func TestIsContended_OtherErrorsAreNot(t *testing.T) {
	if IsContended(nil) || IsContended(os.ErrNotExist) {
		t.Fatal("unrelated errors reported as contended")
	}
}

func TestSameFile_DetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	f := openLockFile(t, path)
	if !SameFile(f, path) {
		t.Fatal("fresh file reported replaced")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if SameFile(f, path) {
		t.Fatal("removed path reported same")
	}
	_ = openLockFile(t, path)
	if SameFile(f, path) {
		t.Fatal("replaced path reported same")
	}
}

func TestTryShared_SharesWithSharedAndExcludesExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	a, b, c := openLockFile(t, path), openLockFile(t, path), openLockFile(t, path)

	if err := TryShared(a); err != nil {
		t.Fatalf("first shared: %v", err)
	}
	if err := TryShared(b); err != nil {
		t.Fatalf("second shared: %v", err)
	}
	if err := TryExclusive(c); !IsContended(err) {
		t.Fatalf("exclusive against shared = %v, want contended", err)
	}
	_ = Unlock(a)
	_ = Unlock(b)
	if err := Exclusive(c); err != nil {
		t.Fatalf("blocking exclusive on a free file: %v", err)
	}
	if err := TryShared(a); !IsContended(err) {
		t.Fatalf("shared against exclusive = %v, want contended", err)
	}
}
