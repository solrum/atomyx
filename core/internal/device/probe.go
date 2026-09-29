package device

import (
	"context"
	"fmt"
	"sync"

	"dev.atomyx/internal/logging"
)

// Probe scans for devices reachable from this host. Scan never
// returns an error: a probe that cannot reach its tooling (adb
// missing, xcrun missing) reports an empty list instead, so a caller
// or a CompositeProbe never has to distinguish "found nothing" from
// "the scanner is broken."
type Probe interface {
	Scan(ctx context.Context) []Descriptor
}

// CompositeProbe aggregates several probes. A failure inside one
// probe's own tooling must not hide devices visible to the others,
// so each probe runs independently and a panic in one is isolated
// from the rest.
type CompositeProbe struct {
	probes []Probe
	logger logging.Logger
}

// NewCompositeProbe constructs a CompositeProbe over probes, logging
// through logger whenever a constituent probe panics. A nil logger
// defaults to logging.NoopLogger{}.
func NewCompositeProbe(logger logging.Logger, probes ...Probe) *CompositeProbe {
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	return &CompositeProbe{probes: probes, logger: logger}
}

var _ Probe = (*CompositeProbe)(nil)

// Scan implements Probe by running every constituent probe
// concurrently and concatenating their results. Result order across
// probes is not guaranteed.
func (c *CompositeProbe) Scan(ctx context.Context) []Descriptor {
	results := make([][]Descriptor, len(c.probes))
	var wg sync.WaitGroup
	for i, p := range c.probes {
		wg.Add(1)
		go func(i int, p Probe) {
			defer wg.Done()
			// A probe that panics instead of returning an empty list is a
			// bug in that probe's own tooling integration, not a "no
			// devices" result, so it is logged here rather than silently
			// folded into an empty scan.
			defer func() {
				if rec := recover(); rec != nil {
					c.logger.Error("device probe panicked", nil, logging.LogContext{
						"probe": fmt.Sprintf("%T", p),
						"panic": fmt.Sprintf("%v", rec),
					})
				}
			}()
			results[i] = p.Scan(ctx)
		}(i, p)
	}
	wg.Wait()

	var out []Descriptor
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

// StaticProbe returns a fixed, scripted sequence of device lists, which is
// the device-free seam a test uses in place of a real adb/xcrun
// scan. Each call to Scan returns the next queued snapshot; once the
// queue is exhausted, Scan returns an empty list.
//
// StaticProbe is safe for concurrent use.
type StaticProbe struct {
	mu        sync.Mutex
	snapshots [][]Descriptor
}

// NewStaticProbe constructs a StaticProbe that returns each of
// snapshots in order, one per Scan call.
func NewStaticProbe(snapshots ...[]Descriptor) *StaticProbe {
	return &StaticProbe{snapshots: append([][]Descriptor(nil), snapshots...)}
}

var _ Probe = (*StaticProbe)(nil)

// Scan implements Probe.
func (p *StaticProbe) Scan(context.Context) []Descriptor {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.snapshots) == 0 {
		return nil
	}
	next := p.snapshots[0]
	p.snapshots = p.snapshots[1:]
	return next
}
