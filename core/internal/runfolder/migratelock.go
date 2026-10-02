package runfolder

import (
	"errors"
	"fmt"
	"os"

	"dev.atomyx/internal/flock"
)

// ErrMigrationInProgress reports that a run migration holds the
// migration lock, so a delete or an edit of a run record must be
// retried once it finishes.
var ErrMigrationInProgress = errors.New("a run migration is in progress")

// MigrationLock is a held lock on the migration lock file of one root.
// The kernel drops it when the process exits, so a crashed migration
// never leaves it stuck.
type MigrationLock struct {
	f *os.File
}

// LockMigration takes the migration lock exclusively. When another
// holder has it, onWait is called once, if not nil, and then
// LockMigration blocks until the lock is free. Only one migration holds it at a time, and none holds it
// while an editor of run records holds the shared form. The lock is
// held per open file, so a second acquisition inside the same process
// conflicts exactly as one in another process would.
func LockMigration(root string, onWait func()) (*MigrationLock, error) {
	f, err := openMigrateLock(root)
	if err != nil {
		return nil, err
	}
	err = flock.TryExclusive(f)
	if err != nil && flock.IsContended(err) {
		if onWait != nil {
			onWait()
		}
		err = flock.Exclusive(f)
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", MigrateLockFileName, err)
	}
	return &MigrationLock{f: f}, nil
}

// ShareMigration takes the migration lock in shared form without
// waiting. Any number of editors hold it together, and it is refused
// with ErrMigrationInProgress while a migration holds the lock.
func ShareMigration(root string) (*MigrationLock, error) {
	f, err := openMigrateLock(root)
	if err != nil {
		return nil, err
	}
	if err := flock.TryShared(f); err != nil {
		_ = f.Close()
		if flock.IsContended(err) {
			return nil, ErrMigrationInProgress
		}
		return nil, fmt.Errorf("locking %s: %w", MigrateLockFileName, err)
	}
	return &MigrationLock{f: f}, nil
}

// Release drops the lock. Releasing twice is harmless.
func (l *MigrationLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

func openMigrateLock(root string) (*os.File, error) {
	if err := os.MkdirAll(RunsDir(root), dirMode); err != nil {
		return nil, fmt.Errorf("creating runs directory: %w", err)
	}
	f, err := os.OpenFile(MigrateLockPath(root), os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", MigrateLockFileName, err)
	}
	return f, nil
}
