package lease

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/flock"
)

func TestRelease_LeavesAReplacementFileInPlace(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, err := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if err != nil {
		t.Fatal(err)
	}
	replaceFile(t, m.LockPath(key))

	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.LockPath(key)); err != nil {
		t.Fatalf("Release removed a file it does not hold: %v", err)
	}
}

func TestRecheck_AfterReleaseDoesNotRecreateTheLockFile(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, _ := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}

	if err := l.Recheck(context.Background()); !errors.Is(err, ErrReleased) {
		t.Fatalf("Recheck err = %v, want ErrReleased", err)
	}
	if _, err := os.Stat(m.LockPath(key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Recheck recreated the lock file: %v", err)
	}
}

func TestAcquire_HolderUnlinkedDuringTheRetryWindowReportsAnUnknownHolder(t *testing.T) {
	clk := &releasingClock{FakeClock: clock.NewFakeClock(0)}
	m := newManagerAt(t.TempDir(), clk)
	key := KeyFor("android", "A")
	first, err := m.Acquire(context.Background(), key, testHolder(1, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()
	clk.onSleep = func() { _ = os.Remove(m.LockPath(key)) }

	_, err = m.Acquire(context.Background(), key, testHolder(2, "b"))

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("err = %v, want HeldError", err)
	}
	if held.Known || held.Holder.Describe() != "unknown process" {
		t.Fatalf("holder = %+v known=%v, want unknown", held.Holder, held.Known)
	}
}

func TestProbe_ReplacedBetweenOpenAndFlockStillSeesTheNewHolder(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	if err := os.WriteFile(m.LockPath(key), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var successor *Lease
	m.afterOpen = func(attempt int) {
		if attempt != 0 {
			return
		}
		if err := os.Remove(m.LockPath(key)); err != nil {
			t.Error(err)
			return
		}
		other := newManagerAt(m.Dir(), clock.NewFakeClock(0))
		l, err := other.Acquire(context.Background(), key, testHolder(88, "successor"))
		if err != nil {
			t.Error(err)
			return
		}
		successor = l
	}

	st, err := m.Probe(context.Background(), key)

	if successor != nil {
		defer func() { _ = successor.Release() }()
	}
	if err != nil || !st.Held || !st.HolderKnown || st.Holder.PID != 88 {
		t.Fatalf("probe = %+v, %v, want held by the successor", st, err)
	}
}

func TestProbe_FileRemovedAfterTheFlockReportsFree(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	if err := os.WriteFile(m.LockPath(key), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.afterOpen = func(int) { _ = os.Remove(m.LockPath(key)) }

	st, err := m.Probe(context.Background(), key)
	if err != nil || st.Held {
		t.Fatalf("probe = %+v, %v, want free", st, err)
	}
}

func TestProbe_PersistentReplacementReturnsTypedError(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	if err := os.WriteFile(m.LockPath(key), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.afterOpen = func(int) { replaceFile(t, m.LockPath(key)) }

	_, err := m.Probe(context.Background(), key)
	if !errors.Is(err, ErrLockFileReplaced) {
		t.Fatalf("err = %v, want ErrLockFileReplaced", err)
	}
}

// leaseWitness tracks the true holder count and which holder is
// current, so a prober can tell whether one holder held the lease for
// a whole window. Every holder gets a distinct nonzero id.
type leaseWitness struct {
	holders atomic.Int32
	nextID  atomic.Int64
	current atomic.Int64
}

func (w *leaseWitness) enter(t *testing.T) {
	t.Helper()
	if n := w.holders.Add(1); n != 1 {
		t.Errorf("holders = %d after an acquire, want 1", n)
	}
	w.current.Store(w.nextID.Add(1))
}

func (w *leaseWitness) leave() {
	w.current.Store(0)
	w.holders.Add(-1)
}

func TestLease_ConcurrentAcquireReleaseKeepsOneHolderAndProbeNeverReportsFree(t *testing.T) {
	const (
		managerWorkers = 6
		rawWorkers     = 2
		cyclesEach     = 60
	)
	dir := t.TempDir()
	key := KeyFor("android", "CHURN")
	var w leaseWitness
	var workers sync.WaitGroup

	for i := 0; i < managerWorkers; i++ {
		m := newManagerAt(dir, clock.SystemClock{})
		workers.Add(1)
		go func() {
			defer workers.Done()
			for done := 0; done < cyclesEach; {
				l, err := m.TryAcquire(context.Background(), key, testHolder(1000+i, "worker"))
				if err != nil {
					var held *HeldError
					if !errors.As(err, &held) && !errors.Is(err, ErrLockFileReplaced) {
						t.Errorf("acquire: %v", err)
						return
					}
					runtime.Gosched()
					continue
				}
				w.enter(t)
				runtime.Gosched()
				w.leave()
				if err := l.Release(); err != nil {
					t.Errorf("release: %v", err)
					return
				}
				done++
			}
		}()
	}
	// A raw opener stands in for a second process that follows the same
	// open, flock, compare protocol and never unlinks the file.
	for i := 0; i < rawWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for done := 0; done < cyclesEach; {
				f, err := os.OpenFile(dirPath(dir, key), os.O_RDWR|os.O_CREATE, fileMode)
				if err != nil {
					t.Errorf("raw open: %v", err)
					return
				}
				if flock.TryExclusive(f) != nil || !flock.SameFile(f, dirPath(dir, key)) {
					_ = f.Close()
					runtime.Gosched()
					continue
				}
				w.enter(t)
				runtime.Gosched()
				w.leave()
				_ = f.Close()
				done++
			}
		}()
	}

	stop := make(chan struct{})
	probed := make(chan struct{})
	go func() {
		defer close(probed)
		m := newManagerAt(dir, clock.SystemClock{})
		// The probe flocks right after the hook returns, so a holder that
		// is current at the hook and still current when the probe returns
		// held the lease at the flock.
		var seen int64
		m.afterOpen = func(int) {
			seen = w.current.Load()
			runtime.Gosched()
		}
		for {
			select {
			case <-stop:
				return
			default:
			}
			seen = 0
			st, err := m.Probe(context.Background(), key)
			if err != nil {
				if !errors.Is(err, ErrLockFileReplaced) {
					t.Errorf("probe: %v", err)
					return
				}
				continue
			}
			if !st.Held && seen != 0 && w.current.Load() == seen {
				t.Error("probe reported free while one holder held the lease throughout")
				return
			}
		}
	}()

	workers.Wait()
	close(stop)
	<-probed
	if n := w.holders.Load(); n != 0 {
		t.Fatalf("holders after all workers finished = %d", n)
	}
}

func dirPath(dir string, key Key) string {
	return newManagerAt(dir, clock.SystemClock{}).LockPath(key)
}
