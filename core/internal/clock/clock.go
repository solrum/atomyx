// Package clock is the time-source abstraction every polling loop,
// timeout, and retry across the driver, orchestra, and provisioning
// packages uses instead of calling time.Now or time.Sleep directly.
// It is a leaf package (imports only the standard library) so any
// package in the module may depend on it without pulling in
// orchestra, a specific driver, or any other business-logic package.
package clock

import (
	"context"
	"time"
)

// Clock is the time source every polling loop, timeout, and retry
// uses instead of calling time.Now or time.Sleep directly. Injecting
// it lets a test run the retry and wait-timeout paths
// deterministically, without waiting on real time.
type Clock interface {
	// NowMs reports the current time as milliseconds since the Unix
	// epoch.
	NowMs() int64
	// Sleep blocks until at least ms milliseconds have elapsed, or
	// ctx is done, whichever comes first. It returns ctx.Err() when
	// ctx is done first, and returns immediately (without blocking)
	// when ms is zero or negative.
	Sleep(ctx context.Context, ms int64) error
}

// SystemClock is the production Clock, backed by the host runtime.
type SystemClock struct{}

func (SystemClock) NowMs() int64 { return time.Now().UnixMilli() }

func (SystemClock) Sleep(ctx context.Context, ms int64) error {
	if ms <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FakeClock is a deterministic Clock for tests. Sleep advances the
// clock's current time immediately instead of blocking on a real
// timer, so a test exercises retry, wait-timeout, and scroll-search
// logic without waiting on real time. FakeClock is not safe for
// concurrent use.
type FakeClock struct {
	current int64
}

// NewFakeClock constructs a FakeClock starting at startMs.
func NewFakeClock(startMs int64) *FakeClock {
	return &FakeClock{current: startMs}
}

func (c *FakeClock) NowMs() int64 { return c.current }

func (c *FakeClock) Sleep(ctx context.Context, ms int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ms > 0 {
		c.current += ms
	}
	return nil
}

// Advance moves the fake clock forward by ms milliseconds without
// going through Sleep. Use it to simulate time passing between two
// calls that don't sleep themselves.
func (c *FakeClock) Advance(ms int64) {
	c.current += ms
}
