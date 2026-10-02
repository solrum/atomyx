package clock

import (
	"context"
	"sync"
)

// GatedClock is a Clock for tests of a watcher with a short retry sleep
// and a long poll sleep. A sleep shorter than the threshold returns at
// once and advances the clock, so a retry loop never waits. A sleep of
// at least the threshold blocks until Fire, so a test decides when the
// poll runs.
type GatedClock struct {
	thresholdMs int64
	gate        chan struct{}

	mu  sync.Mutex
	now int64
}

// NewGatedClock returns a GatedClock that gates sleeps of thresholdMs or
// longer.
func NewGatedClock(thresholdMs int64) *GatedClock {
	return &GatedClock{thresholdMs: thresholdMs, gate: make(chan struct{})}
}

// NowMs reports the sum of the short sleeps so far.
func (c *GatedClock) NowMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep implements Clock.
func (c *GatedClock) Sleep(ctx context.Context, ms int64) error {
	if ms >= c.thresholdMs {
		select {
		case <-c.gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	c.now += ms
	c.mu.Unlock()
	return ctx.Err()
}

// Fire releases one sleeper that is waiting at the gate, and blocks
// until one is.
func (c *GatedClock) Fire() { c.gate <- struct{}{} }
