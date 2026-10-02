package runfolder

import (
	"os"

	"dev.atomyx/internal/flock"
)

// LockLiveness is a Liveness that answers from the lock file of the
// run folder, so it sees a run written by any process on the machine.
// A run is live while some open file holds the lock, and the kernel
// drops the lock when the writer exits for any reason, so a crashed
// writer reads as not live.
type LockLiveness struct {
	root string
}

// NewLockLiveness returns a LockLiveness for the run folders under root.
func NewLockLiveness(root string) *LockLiveness {
	return &LockLiveness{root: root}
}

// IsLive reports whether a writer currently holds the run's lock. A
// missing run, a missing lock file, and an unreadable one all report
// false, because none of them proves a live writer. It never creates
// or writes the lock file.
func (l *LockLiveness) IsLive(runID string) bool {
	if !ValidRunID(runID) {
		return false
	}
	f, err := os.OpenFile(LockPath(RunDir(l.root, runID)), os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if err := flock.TryExclusive(f); err != nil {
		return flock.IsContended(err)
	}
	_ = flock.Unlock(f)
	return false
}
