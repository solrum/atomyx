package lease

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
)

const handoffObserveTimeout = 10 * time.Second

// sleepCountingClock counts the sleeps a caller takes between contended
// flock attempts. A caller that has slept is blocked behind a holder.
type sleepCountingClock struct {
	clock.SystemClock
	sleeps *atomic.Int64
}

func (c sleepCountingClock) Sleep(ctx context.Context, ms int64) error {
	c.sleeps.Add(1)
	return c.SystemClock.Sleep(ctx, ms)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(handoffObserveTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestAcquire_QueuedWaitersAllAcquireAcrossFastHandoffs queues more waiters
// behind one holder than maxAcquireAttempts. Each handoff unlinks the lock
// file under every remaining waiter, so a waiter that counted each of
// those against the cap would fail with ErrLockFileReplaced.
func TestAcquire_QueuedWaitersAllAcquireAcrossFastHandoffs(t *testing.T) {
	const waiters = maxAcquireAttempts*2 + 1
	dir := t.TempDir()
	key := KeyFor("android", "HANDOFF")
	first, err := newManagerAt(dir, clock.SystemClock{}).Acquire(context.Background(), key, testHolder(1, "first"))
	if err != nil {
		t.Fatal(err)
	}

	sleeps := make([]atomic.Int64, waiters)
	release := make([]chan struct{}, waiters)
	acquired := make(chan int, waiters)
	failed := make(chan error, waiters)
	var done sync.WaitGroup
	for i := 0; i < waiters; i++ {
		release[i] = make(chan struct{})
		m := newManagerAt(dir, sleepCountingClock{sleeps: &sleeps[i]})
		done.Add(1)
		go func() {
			defer done.Done()
			l, err := m.Acquire(context.Background(), key, testHolder(100+i, "waiter"))
			if err != nil {
				failed <- err
				return
			}
			acquired <- i
			<-release[i]
			if err := l.Release(); err != nil {
				failed <- err
			}
		}()
	}

	blocked := func(pending map[int]bool, since []int64) func() bool {
		return func() bool {
			if len(failed) > 0 {
				return true
			}
			for i := range pending {
				if sleeps[i].Load() <= since[i] {
					return false
				}
			}
			return true
		}
	}
	pending := map[int]bool{}
	for i := 0; i < waiters; i++ {
		pending[i] = true
	}
	eventually(t, "every waiter to block behind the first holder", blocked(pending, make([]int64, waiters)))
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}

	for len(pending) > 0 {
		select {
		case i := <-acquired:
			delete(pending, i)
			since := make([]int64, waiters)
			for j := range since {
				since[j] = sleeps[j].Load()
			}
			eventually(t, "the remaining waiters to block behind the new holder", blocked(pending, since))
			close(release[i])
		case err := <-failed:
			t.Fatalf("a queued waiter failed instead of waiting its turn: %v", err)
		case <-time.After(handoffObserveTimeout):
			t.Fatalf("timed out with %d waiters still queued", len(pending))
		}
	}
	done.Wait()
	select {
	case err := <-failed:
		t.Fatalf("release: %v", err)
	default:
	}
}

// TestProbe_WaitingProbeSurvivesFastHandoffs keeps a probe blocked behind a
// holder while the lease changes hands more often than maxAcquireAttempts.
func TestProbe_WaitingProbeSurvivesFastHandoffs(t *testing.T) {
	const handoffs = maxAcquireAttempts*2 + 1
	dir := t.TempDir()
	key := KeyFor("android", "PROBEHANDOFF")
	holder := newManagerAt(dir, clock.SystemClock{})
	current, err := holder.Acquire(context.Background(), key, testHolder(1, "holder"))
	if err != nil {
		t.Fatal(err)
	}

	var sleeps atomic.Int64
	type probeResult struct {
		status Status
		err    error
	}
	result := make(chan probeResult, 1)
	prober := newManagerAt(dir, sleepCountingClock{sleeps: &sleeps})
	go func() {
		status, err := prober.Probe(context.Background(), key)
		result <- probeResult{status, err}
	}()

	for round := 0; round < handoffs; round++ {
		before := sleeps.Load()
		eventually(t, "the probe to block behind the holder", func() bool { return sleeps.Load() > before || len(result) > 0 })
		if len(result) > 0 {
			break
		}
		if err := current.Release(); err != nil {
			t.Fatal(err)
		}
		next, err := holder.TryAcquire(context.Background(), key, testHolder(2+round, "holder"))
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		current = next
	}
	got := <-result
	_ = current.Release()
	if got.err != nil {
		t.Fatalf("probe failed across fast handoffs: %v", got.err)
	}
}
