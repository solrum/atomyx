package lease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/flock"
)

const (
	keyHexLen = 16

	// InodeRecheckPeriod is how often a holder re-compares its lock file
	// descriptor with the path, because the file could be replaced under
	// a long-lived holder.
	InodeRecheckPeriod = 5 * time.Second

	// AcquireRetryWindow is how long an acquirer keeps retrying a
	// contended flock before reporting a conflict. A process that is
	// mid-acquire or about to exit can hold the flock for a few
	// milliseconds without being a genuine conflict.
	AcquireRetryWindow = 200 * time.Millisecond

	// AcquirePollInterval is the pause between flock attempts inside the
	// retry window.
	AcquirePollInterval = 25 * time.Millisecond

	// maxAcquireAttempts bounds how many times acquire or probe restarts
	// after finding the lock file replaced by a process that did not make
	// the caller wait. A restart after the caller waited out a holder is
	// not counted, see lockFile.
	maxAcquireAttempts = 3
)

// ErrLockFileReplaced reports that another process replaced the lock
// file between open and flock on every bounded attempt.
var ErrLockFileReplaced = errors.New("lease lock file was replaced during acquire")

// ErrReleased reports use of a Lease after Release.
var ErrReleased = errors.New("lease is released")

// ErrLost reports that a holder's lock file was replaced and the
// re-acquire that followed also failed, so the device is no longer
// held. It wraps the acquire failure.
var ErrLost = errors.New("lease lost")

// Key identifies one device's lease files.
type Key string

// KeyFor derives the lease key for a device: the first 16 hex
// characters of the SHA-256 of "<platform>:<hardwareID>".
func KeyFor(platform, hardwareID string) Key {
	sum := sha256.Sum256([]byte(platform + ":" + hardwareID))
	return Key(hex.EncodeToString(sum[:])[:keyHexLen])
}

// Manager acquires and probes leases inside one runtime directory.
type Manager struct {
	dir   string
	clock clock.Clock

	// afterOpen runs between opening the lock file and taking the flock,
	// so a test can replace the file at that exact moment.
	afterOpen func(attempt int)
}

// NewManager resolves the runtime directory from env and returns a
// Manager over it. clk drives the retry window and the Watch loop.
func NewManager(env Environment, clk clock.Clock) (*Manager, error) {
	dir, err := ResolveRuntimeDir(env)
	if err != nil {
		return nil, err
	}
	return newManagerAt(dir, clk), nil
}

func newManagerAt(dir string, clk clock.Clock) *Manager {
	return &Manager{dir: dir, clock: clk}
}

// Dir reports the runtime directory.
func (m *Manager) Dir() string { return m.dir }

// LockPath reports the lock file path for key.
func (m *Manager) LockPath(key Key) string { return filepath.Join(m.dir, string(key)+".lock") }

// SocketPath reports the socket path for key.
func (m *Manager) SocketPath(key Key) string { return filepath.Join(m.dir, string(key)+".sock") }

// Lease is a held device lease. The flock lives on an open descriptor
// until Release or process exit.
type Lease struct {
	m    *Manager
	key  Key
	path string

	mu     sync.Mutex
	file   *os.File
	holder Holder
}

// Acquire takes the exclusive lease for key and records holder in the
// lock file. A contended flock is retried for about 200 milliseconds,
// then reported as a *HeldError carrying DEVICE_HELD with the current
// holder's metadata. After the flock succeeds, a lock file replaced
// since it was opened forces a restart. Restarts after a wait for a
// holder are unbounded because each follows a handoff, and persistent
// replacement of a lock file that was never waited on returns
// ErrLockFileReplaced.
func (m *Manager) Acquire(ctx context.Context, key Key, holder Holder) (*Lease, error) {
	return m.acquire(ctx, key, holder, AcquireRetryWindow)
}

// TryAcquire takes the exclusive lease for key with one non-blocking
// flock attempt and no retry window. A lease that is held, even for an
// instant, is reported at once as a *HeldError.
func (m *Manager) TryAcquire(ctx context.Context, key Key, holder Holder) (*Lease, error) {
	return m.acquire(ctx, key, holder, 0)
}

func (m *Manager) acquire(ctx context.Context, key Key, holder Holder, retryWindow time.Duration) (*Lease, error) {
	path := m.LockPath(key)
	f, err := m.lockFile(ctx, path, retryWindow)
	if err != nil {
		return nil, err
	}
	l := &Lease{m: m, key: key, path: path, file: f, holder: holder}
	if err := l.writeHolderLocked(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return l, nil
}

// lockFile opens and flocks the lock file at path, restarting when the
// file turns out to have been replaced.
//
// A restart is counted against maxAcquireAttempts only when the flock was
// taken without waiting. A caller that had to wait for a holder and then
// finds its file unlinked has seen the lease change hands, which is the
// normal end of a handoff and not interference, so with several waiters
// queued behind one holder each of them can see the file go stale once per
// handoff ahead of it. Those restarts cannot loop without bound, because
// each one needs another holder to take and release the lease, and every
// wait is bounded by the retry window and the context. A file replaced
// under a caller that never waited has no such progress behind it, so
// those restarts stay capped.
func (m *Manager) lockFile(ctx context.Context, path string, retryWindow time.Duration) (*os.File, error) {
	counted := 0
	for opened := 0; counted < maxAcquireAttempts; opened++ {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, fileMode)
		if err != nil {
			return nil, fmt.Errorf("open lease file %s: %w", path, err)
		}
		if m.afterOpen != nil {
			m.afterOpen(opened)
		}
		waited, err := m.flockWithRetry(ctx, f, retryWindow)
		if err != nil {
			_ = f.Close()
			if flock.IsContended(err) {
				h, known := readHolder(path)
				return nil, newHeldError(h, known)
			}
			return nil, err
		}
		if flock.SameFile(f, path) {
			return f, nil
		}
		_ = f.Close()
		if !waited {
			counted++
		}
	}
	return nil, ErrLockFileReplaced
}

// flockWithRetry takes the exclusive flock on f, polling a contended lock
// for up to retryWindow. waited reports whether the lock was contended at
// least once before it was taken.
func (m *Manager) flockWithRetry(ctx context.Context, f *os.File, retryWindow time.Duration) (waited bool, err error) {
	deadline := m.clock.NowMs() + retryWindow.Milliseconds()
	for {
		err := flock.TryExclusive(f)
		if err == nil {
			return waited, nil
		}
		if !flock.IsContended(err) {
			return waited, fmt.Errorf("flock lease file: %w", err)
		}
		if m.clock.NowMs() >= deadline {
			return waited, err
		}
		waited = true
		if err := m.clock.Sleep(ctx, AcquirePollInterval.Milliseconds()); err != nil {
			return waited, err
		}
	}
}

// Key reports the lease key.
func (l *Lease) Key() Key { return l.key }

// Holder reports the metadata last written to the lock file.
func (l *Lease) Holder() Holder {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.holder
}

// UpdateHolder applies mutate to the holder metadata and rewrites the
// lock file.
func (l *Lease) UpdateHolder(mutate func(*Holder)) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrReleased
	}
	mutate(&l.holder)
	return l.writeHolderLocked()
}

func (l *Lease) writeHolderLocked() error {
	data, err := json.Marshal(l.holder)
	if err != nil {
		return fmt.Errorf("encode lease holder: %w", err)
	}
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("write lease holder: %w", err)
	}
	if _, err := l.file.WriteAt(data, 0); err != nil {
		return fmt.Errorf("write lease holder: %w", err)
	}
	return nil
}

// Verify compares the held descriptor with the lock file path and
// reports false when the file was replaced. It returns ErrReleased
// after Release.
func (l *Lease) Verify() (intact bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return false, ErrReleased
	}
	return flock.SameFile(l.file, l.path), nil
}

// Recheck verifies the lock file is still the one this lease holds. On
// a replaced file it closes the stale descriptor and re-acquires from
// the top. A failed re-acquire returns an error wrapping ErrLost.
func (l *Lease) Recheck(ctx context.Context) error {
	intact, err := l.Verify()
	if err != nil || intact {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return ErrReleased
	}
	_ = l.file.Close()
	l.file = nil
	f, err := l.m.lockFile(ctx, l.path, AcquireRetryWindow)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLost, err)
	}
	l.file = f
	if err := l.writeHolderLocked(); err != nil {
		_ = f.Close()
		l.file = nil
		return fmt.Errorf("%w: %w", ErrLost, err)
	}
	return nil
}

// Watch runs Recheck every InodeRecheckPeriod until ctx ends or the
// lease is released. It reports a Recheck failure to onLost once and
// returns. It blocks, so a caller runs it in its own goroutine.
func (l *Lease) Watch(ctx context.Context, onLost func(error)) {
	for {
		if err := l.m.clock.Sleep(ctx, InodeRecheckPeriod.Milliseconds()); err != nil {
			return
		}
		err := l.Recheck(ctx)
		if errors.Is(err, ErrReleased) {
			return
		}
		if err != nil {
			onLost(err)
			return
		}
	}
}

// Release clears the holder metadata, unlinks the lock file while the
// flock is still held, and then gives up the lease. The file is
// unlinked only when the path still names the descriptor this lease
// holds, so a replacement file owned by another process is never
// removed. A process that opened the old file before the unlink wins
// the flock on an inode no path names, notices the mismatch, and
// restarts on a fresh file. Releasing twice is a no-op.
func (l *Lease) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	truncErr := f.Truncate(0)
	var unlinkErr error
	if flock.SameFile(f, l.path) {
		unlinkErr = os.Remove(l.path)
		if errors.Is(unlinkErr, os.ErrNotExist) {
			unlinkErr = nil
		}
	}
	closeErr := f.Close()
	return errors.Join(truncErr, unlinkErr, closeErr)
}

// Status is the result of a Probe.
type Status struct {
	// Held reports whether another process holds the lease.
	Held bool
	// Holder is the display metadata of the holder when Held and the
	// metadata was readable.
	Holder Holder
	// HolderKnown reports whether Holder was read.
	HolderKnown bool
}

// Probe reports whether key's lease is held, using the same real flock
// an acquirer takes, released immediately. It never creates or writes
// the lock file, so a device that was never leased reports free
// without leaving a file behind. Like an acquirer, it compares the
// descriptor with the path after the flock succeeds and restarts when
// the file was replaced, so it never reports free while a new holder
// owns the replacement. Persistent replacement returns
// ErrLockFileReplaced.
func (m *Manager) Probe(ctx context.Context, key Key) (Status, error) {
	path := m.LockPath(key)
	counted := 0
	for opened := 0; counted < maxAcquireAttempts; opened++ {
		status, outcome, err := m.probeOnce(ctx, path, opened)
		if err != nil || outcome == probeSettled {
			return status, err
		}
		if outcome == probeReplaced {
			counted++
		}
	}
	return Status{}, ErrLockFileReplaced
}

type probeOutcome int

const (
	// probeSettled means the probe has its answer.
	probeSettled probeOutcome = iota
	// probeReplaced means the file was replaced under a probe that never
	// waited, so the restart counts against the cap.
	probeReplaced
	// probeHandedOff means the file was replaced after the probe waited out
	// a holder, so the restart is not counted, as in lockFile.
	probeHandedOff
)

// probeOnce runs one probe attempt. Its outcome is not probeSettled when
// the file was replaced after the flock succeeded and the caller should
// retry.
func (m *Manager) probeOnce(ctx context.Context, path string, attempt int) (Status, probeOutcome, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, probeSettled, nil
	}
	if err != nil {
		return Status{}, probeSettled, fmt.Errorf("open lease file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if m.afterOpen != nil {
		m.afterOpen(attempt)
	}
	waited, err := m.flockWithRetry(ctx, f, AcquireRetryWindow)
	if err != nil {
		if flock.IsContended(err) {
			h, known := readHolder(path)
			return Status{Held: true, Holder: h, HolderKnown: known}, probeSettled, nil
		}
		return Status{}, probeSettled, err
	}
	same := flock.SameFile(f, path)
	if err := flock.Unlock(f); err != nil {
		return Status{}, probeSettled, fmt.Errorf("unlock probed lease file: %w", err)
	}
	switch {
	case same || !pathExists(path):
		return Status{}, probeSettled, nil
	case waited:
		return Status{}, probeHandedOff, nil
	default:
		return Status{}, probeReplaced, nil
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
