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
	"syscall"
	"time"

	"dev.atomyx/internal/clock"
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

	// maxAcquireAttempts bounds how many times acquire restarts after
	// finding the lock file replaced.
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
// since it was opened forces a bounded restart, and persistent
// replacement returns ErrLockFileReplaced.
func (m *Manager) Acquire(ctx context.Context, key Key, holder Holder) (*Lease, error) {
	path := m.LockPath(key)
	f, err := m.lockFile(ctx, path)
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

func (m *Manager) lockFile(ctx context.Context, path string) (*os.File, error) {
	for attempt := 0; attempt < maxAcquireAttempts; attempt++ {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, fileMode)
		if err != nil {
			return nil, fmt.Errorf("open lease file %s: %w", path, err)
		}
		if m.afterOpen != nil {
			m.afterOpen(attempt)
		}
		if err := m.flockWithRetry(ctx, f); err != nil {
			_ = f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				h, known := readHolder(path)
				return nil, newHeldError(h, known)
			}
			return nil, err
		}
		if sameAsPath(f, path) {
			return f, nil
		}
		_ = f.Close()
	}
	return nil, ErrLockFileReplaced
}

func (m *Manager) flockWithRetry(ctx context.Context, f *os.File) error {
	deadline := m.clock.NowMs() + AcquireRetryWindow.Milliseconds()
	for {
		err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("flock lease file: %w", err)
		}
		if m.clock.NowMs() >= deadline {
			return err
		}
		if err := m.clock.Sleep(ctx, AcquirePollInterval.Milliseconds()); err != nil {
			return err
		}
	}
}

func sameAsPath(f *os.File, path string) bool {
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
	return sameAsPath(l.file, l.path), nil
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
	f, err := l.m.lockFile(ctx, l.path)
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

// Release clears the holder metadata and gives up the lease. It does
// not unlink the lock file. Releasing twice is a no-op.
func (l *Lease) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	truncErr := f.Truncate(0)
	closeErr := f.Close()
	return errors.Join(truncErr, closeErr)
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
// without leaving a file behind.
func (m *Manager) Probe(ctx context.Context, key Key) (Status, error) {
	path := m.LockPath(key)
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("open lease file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := m.flockWithRetry(ctx, f); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			h, known := readHolder(path)
			return Status{Held: true, Holder: h, HolderKnown: known}, nil
		}
		return Status{}, err
	}
	if err := flock(f, syscall.LOCK_UN); err != nil {
		return Status{}, fmt.Errorf("unlock probed lease file: %w", err)
	}
	return Status{}, nil
}
