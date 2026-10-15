package clock

import (
	"context"
	"sync"
)

// ManualClock is a Clock whose Sleep blocks until a test advances
// time past the sleeper's deadline, so a timer-driven code path runs
// deterministically without waiting on real time. Unlike FakeClock it
// is safe for concurrent use, which a watchdog goroutine needs.
type ManualClock struct {
	mu       sync.Mutex
	cond     *sync.Cond
	current  int64
	sleepers []*manualSleeper
}

type manualSleeper struct {
	deadline int64
	wake     chan struct{}
	ctx      context.Context
}

// NewManualClock constructs a ManualClock starting at startMs.
func NewManualClock(startMs int64) *ManualClock {
	c := &ManualClock{current: startMs}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *ManualClock) NowMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Sleep blocks until Advance moves the clock ms milliseconds past the
// moment Sleep was called, or ctx is done. A zero or negative ms
// returns immediately.
func (c *ManualClock) Sleep(ctx context.Context, ms int64) error {
	if ms <= 0 {
		return ctx.Err()
	}
	c.mu.Lock()
	sleeper := &manualSleeper{deadline: c.current + ms, wake: make(chan struct{}), ctx: ctx}
	c.sleepers = append(c.sleepers, sleeper)
	c.cond.Broadcast()
	c.mu.Unlock()

	select {
	case <-sleeper.wake:
		return nil
	case <-ctx.Done():
		c.remove(sleeper)
		return ctx.Err()
	}
}

func (c *ManualClock) remove(target *manualSleeper) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.sleepers {
		if s == target {
			c.sleepers = append(c.sleepers[:i], c.sleepers[i+1:]...)
			c.cond.Broadcast()
			return
		}
	}
}

// AwaitSleepers blocks until at least n goroutines are inside Sleep
// with a context that is still live, so a test can advance time only
// after the code under test has registered its timer. A sleeper whose
// context was already canceled is on its way out and does not count,
// because it can no longer be woken by Advance.
func (c *ManualClock) AwaitSleepers(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.liveSleepersLocked() < n {
		c.cond.Wait()
	}
}

// AwaitSleeperAt blocks until a goroutine with a still-live context is
// inside Sleep with exactly the given deadline, in clock milliseconds.
// Unlike AwaitSleepers it cannot be satisfied by a different sleeper,
// so a test that depends on one specific timer being armed, such as a
// budget timer or a progress ticker, advances time only after that
// timer exists. A sleeper's deadline is the clock reading when Sleep
// was called plus its duration, so the caller must not advance the
// clock between arming the timer and waiting for it.
func (c *ManualClock) AwaitSleeperAt(deadlineMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for !c.hasLiveSleeperAtLocked(deadlineMs) {
		c.cond.Wait()
	}
}

func (c *ManualClock) hasLiveSleeperAtLocked(deadlineMs int64) bool {
	for _, s := range c.sleepers {
		if s.deadline == deadlineMs && s.ctx.Err() == nil {
			return true
		}
	}
	return false
}

func (c *ManualClock) liveSleepersLocked() int {
	live := 0
	for _, s := range c.sleepers {
		if s.ctx.Err() == nil {
			live++
		}
	}
	return live
}

// AwaitIdle blocks until no goroutine is inside Sleep, so a test can
// tell that a canceled timer has been fully released.
func (c *ManualClock) AwaitIdle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.sleepers) > 0 {
		c.cond.Wait()
	}
}

// Advance moves the clock forward by ms and wakes every sleeper whose
// deadline has been reached.
func (c *ManualClock) Advance(ms int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current += ms
	remaining := c.sleepers[:0]
	for _, s := range c.sleepers {
		if s.deadline <= c.current && s.ctx.Err() == nil {
			close(s.wake)
			continue
		}
		remaining = append(remaining, s)
	}
	c.sleepers = remaining
	c.cond.Broadcast()
}

var _ Clock = (*ManualClock)(nil)
